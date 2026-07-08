// Package dockerengine spins up an ephemeral database container, restores a
// dump into it, and guarantees the container is removed after use.
//
// Postgres and MySQL clients run INSIDE the container via the Docker exec
// API — not on the agent host. This removes the agent's dependency on
// postgresql-client/mysql-client: the required binaries already ship in the
// postgres:18/mysql:8 images themselves.
package dockerengine

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

const (
	postgresImage     = "postgres:18"
	mysqlImage        = "mysql:8"
	dbName            = "undump_check"
	pgUser            = "undump"
	readyTimeout      = 60 * time.Second
	containerDumpPath = "/tmp/dump"
)

// engineSpec describes everything that varies per database engine when
// spinning up the ephemeral container and restoring into it.
type engineSpec struct {
	image        string
	port         string // e.g. "5432/tcp"
	tmpfsPath    string // ephemeral storage — nothing touches disk
	containerEnv func(password string) []string
	execEnv      func(password string) []string // env for docker-exec'd commands (ready check + restore)
	readyCmd     []string
	restoreCmd   func(containerDumpPath string) []string
	queryCmd     func(query string) []string
	dsn          func(password, host, port string) string
}

func specFor(engine Engine) engineSpec {
	if engine == EngineMySQL {
		return mysqlSpec
	}
	return postgresSpec(engine == EnginePostgresCustom)
}

func postgresSpec(custom bool) engineSpec {
	restoreCmd := func(path string) []string {
		return []string{"psql", "-U", pgUser, "-d", dbName, "--set", "ON_ERROR_STOP=1", "-f", path}
	}
	if custom {
		restoreCmd = func(path string) []string {
			return []string{"pg_restore", "--no-owner", "--no-acl", "-U", pgUser, "-d", dbName, path}
		}
	}
	return engineSpec{
		image:     postgresImage,
		port:      "5432/tcp",
		tmpfsPath: "/var/lib/postgresql",
		containerEnv: func(password string) []string {
			return []string{"POSTGRES_PASSWORD=" + password, "POSTGRES_USER=" + pgUser, "POSTGRES_DB=" + dbName}
		},
		execEnv:    func(password string) []string { return nil },
		readyCmd:   []string{"pg_isready", "-U", pgUser, "-d", dbName},
		restoreCmd: restoreCmd,
		queryCmd: func(query string) []string {
			return []string{"psql", "-U", pgUser, "-d", dbName, "--set", "ON_ERROR_STOP=1", "-t", "-A", "-c", query}
		},
		dsn: func(password, host, port string) string {
			return fmt.Sprintf("postgresql://%s:%s@%s:%s/%s", pgUser, password, host, port, dbName)
		},
	}
}

var mysqlSpec = engineSpec{
	image:     mysqlImage,
	port:      "3306/tcp",
	tmpfsPath: "/var/lib/mysql",
	containerEnv: func(password string) []string {
		return []string{"MYSQL_ROOT_PASSWORD=" + password, "MYSQL_DATABASE=" + dbName}
	},
	execEnv: func(password string) []string {
		return []string{"MYSQL_PWD=" + password}
	},
	// A plain "mysqladmin ping" isn't enough: the entrypoint briefly runs an
	// unauthenticated bootstrap mysqld on the same socket before the real
	// server (with MYSQL_ROOT_PASSWORD applied) takes over, and ping
	// succeeds against that bootstrap instance too. Requiring an
	// authenticated query means readiness only trips once the real
	// password-protected server is actually up.
	readyCmd: []string{"mysql", "-uroot", "-e", "SELECT 1"},
	restoreCmd: func(path string) []string {
		// mysql has no --file flag for plain SQL; shell redirection is the
		// standard way to feed a dump to the client.
		return []string{"sh", "-c", fmt.Sprintf("mysql -uroot %s < %s", dbName, path)}
	},
	queryCmd: func(query string) []string {
		return []string{"mysql", "-uroot", "-N", "-B", dbName, "-e", query}
	},
	dsn: func(password, host, port string) string {
		return fmt.Sprintf("mysql://root:%s@%s:%s/%s", password, host, port, dbName)
	},
}

// Outcome — result of a restore attempt.
type Outcome struct {
	OK         bool
	RTOSeconds float64
	Detail     string
}

// Session — an ephemeral database with the dump restored (or not — see Outcome) into it.
// The caller MUST call Close(), ideally via defer right after Restore.
type Session struct {
	Outcome Outcome
	DSN     string

	cli         *dockerclient.Client
	containerID string
	password    string
	spec        engineSpec
	engine      Engine
}

// EngineName reports the engine actually detected from the dump ("postgres"
// or "mysql"). The target config's engine field is only a reporting label —
// checks that build engine-specific SQL must use this instead.
func (s *Session) EngineName() string {
	if s.engine == EngineMySQL {
		return "mysql"
	}
	return "postgres"
}

// Restore detects the dump's engine, spins up a throwaway container for it,
// restores dumpPath into it, and returns a Session. An error is returned
// ONLY if the infrastructure itself could not be brought up/reached (Docker
// unavailable, readiness timeout, etc.) — if the restore of the dump itself
// fails, that's reflected in Session.Outcome.OK=false, not in err, so the
// caller can still build a RunReport with status "fail".
func Restore(ctx context.Context, dumpPath string) (*Session, error) {
	engine, err := detectEngine(dumpPath)
	if err != nil {
		return nil, fmt.Errorf("detecting dump engine: %w", err)
	}
	spec := specFor(engine)

	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("connecting to Docker: %w", err)
	}

	if err := ensureImage(ctx, cli, spec.image); err != nil {
		if cerr := cli.Close(); cerr != nil {
			slog.Warn("failed to close Docker client after image pull error", "error", cerr)
		}
		return nil, err
	}

	started := time.Now()
	password := randomPassword()

	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image:  spec.image,
			Env:    spec.containerEnv(password),
			Labels: map[string]string{"app": "undump", "ephemeral": "true"},
		},
		&container.HostConfig{
			// postgres:18+ images refuse to start with anything mounted at the old
			// /var/lib/postgresql/data path — they expect PGDATA to live in a
			// major-version-specific subdirectory under /var/lib/postgresql itself.
			Tmpfs: map[string]string{spec.tmpfsPath: ""},
			PortBindings: nat.PortMap{
				nat.Port(spec.port): []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: ""}},
			},
		},
		&network.NetworkingConfig{}, nil, "",
	)
	if err != nil {
		if cerr := cli.Close(); cerr != nil {
			slog.Warn("failed to close Docker client after container creation error", "error", cerr)
		}
		return nil, fmt.Errorf("creating container: %w", err)
	}

	session := &Session{cli: cli, containerID: resp.ID, password: password, spec: spec, engine: engine}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		if cerr := session.Close(); cerr != nil {
			slog.Warn("failed to remove container after start error", "error", cerr)
		}
		return nil, fmt.Errorf("starting container: %w", err)
	}

	hostPort, err := session.waitReady(ctx, password)
	if err != nil {
		if cerr := session.Close(); cerr != nil {
			slog.Warn("failed to remove container after readiness error", "error", cerr)
		}
		return nil, err
	}
	session.DSN = spec.dsn(password, "127.0.0.1", hostPort)

	ok, detail, err := session.restoreDump(ctx, dumpPath, password)
	if err != nil {
		if cerr := session.Close(); cerr != nil {
			slog.Warn("failed to remove container after restore error", "error", cerr)
		}
		return nil, err
	}
	session.Outcome = Outcome{
		OK:         ok,
		RTOSeconds: time.Since(started).Seconds(),
		Detail:     detail,
	}
	return session, nil
}

// QueryScalar runs a query inside the restored database container and returns
// the trimmed text output. It intentionally uses the client binaries inside
// the DB container, matching restore behavior and avoiding network reachability
// assumptions from the agent process.
func (s *Session) QueryScalar(ctx context.Context, query string) (string, error) {
	code, output, err := s.exec(ctx, s.spec.queryCmd(query), s.spec.execEnv(s.password))
	if err != nil {
		return "", fmt.Errorf("querying restored database: %w", err)
	}
	output = strings.TrimSpace(output)
	if code != 0 {
		return "", fmt.Errorf("query rc=%d: %s", code, truncate(output, 500))
	}
	return output, nil
}

// Close GUARANTEES the container is removed, even if it's in a broken state.
func (s *Session) Close() error {
	if s.cli == nil {
		return nil
	}
	defer func() {
		if cerr := s.cli.Close(); cerr != nil {
			slog.Warn("failed to close Docker client", "error", cerr)
		}
	}()
	if s.containerID == "" {
		return nil
	}
	return s.cli.ContainerRemove(context.Background(), s.containerID,
		container.RemoveOptions{Force: true, RemoveVolumes: true})
}

func (s *Session) waitReady(ctx context.Context, password string) (hostPort string, err error) {
	deadline := time.Now().Add(readyTimeout)
	for time.Now().Before(deadline) {
		code, _, execErr := s.exec(ctx, s.spec.readyCmd, s.spec.execEnv(password))
		if execErr == nil && code == 0 {
			inspect, inspectErr := s.cli.ContainerInspect(ctx, s.containerID)
			if inspectErr != nil {
				return "", fmt.Errorf("inspecting container: %w", inspectErr)
			}
			bindings := inspect.NetworkSettings.Ports[nat.Port(s.spec.port)]
			if len(bindings) == 0 {
				return "", fmt.Errorf("port %s not published", s.spec.port)
			}
			return bindings[0].HostPort, nil
		}
		time.Sleep(time.Second)
	}
	return "", fmt.Errorf("%s did not become ready within %s", s.spec.image, readyTimeout)
}

func (s *Session) restoreDump(ctx context.Context, dumpPath string, password string) (ok bool, detail string, err error) {
	if err := s.copyToContainer(ctx, dumpPath, containerDumpPath); err != nil {
		return false, "", fmt.Errorf("copying dump into container: %w", err)
	}

	code, output, err := s.exec(ctx, s.spec.restoreCmd(containerDumpPath), s.spec.execEnv(password))
	if err != nil {
		return false, "", fmt.Errorf("restoring: %w", err)
	}
	if code != 0 {
		return false, fmt.Sprintf("restore rc=%d: %s", code, truncate(output, 500)), nil
	}
	return true, "restore completed without errors", nil
}

func (s *Session) exec(ctx context.Context, cmd []string, env []string) (exitCode int, output string, err error) {
	execCfg := container.ExecOptions{Cmd: cmd, Env: env, AttachStdout: true, AttachStderr: true}
	execID, err := s.cli.ContainerExecCreate(ctx, s.containerID, execCfg)
	if err != nil {
		return -1, "", err
	}
	attach, err := s.cli.ContainerExecAttach(ctx, execID.ID, container.ExecAttachOptions{})
	if err != nil {
		return -1, "", err
	}
	defer attach.Close()

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, attach.Reader); err != nil {
		return -1, "", err
	}

	inspect, err := s.cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return -1, "", err
	}
	return inspect.ExitCode, stdout.String() + stderr.String(), nil
}

func (s *Session) copyToContainer(ctx context.Context, hostPath, containerPath string) error {
	data, err := os.ReadFile(hostPath)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: containerPath, Mode: 0644, Size: int64(len(data))}); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return s.cli.CopyToContainer(ctx, s.containerID, "/", &buf, container.CopyToContainerOptions{})
}

// ensureImage pulls the image if it is not already present locally. A pull
// failure is an infrastructure error (like Docker being unreachable), not a
// restore failure. The pull happens BEFORE the RTO timer starts — a cold
// image cache shouldn't inflate the measured restore time.
func ensureImage(ctx context.Context, cli *dockerclient.Client, ref string) error {
	_, err := cli.ImageInspect(ctx, ref)
	if err == nil {
		return nil
	}
	if !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("inspecting image %s: %w", ref, err)
	}

	slog.Info("image not present locally, pulling", "image", ref)
	rc, err := cli.ImagePull(ctx, ref, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pulling image %s: %w", ref, err)
	}
	defer func() {
		if cerr := rc.Close(); cerr != nil {
			slog.Warn("failed to close image pull stream", "image", ref, "error", cerr)
		}
	}()
	// The pull only completes once the progress stream is fully consumed.
	if _, err := io.Copy(io.Discard, rc); err != nil {
		return fmt.Errorf("pulling image %s: %w", ref, err)
	}
	slog.Info("image pulled", "image", ref)
	return nil
}

func randomPassword() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable: %s", err))
	}
	return hex.EncodeToString(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
