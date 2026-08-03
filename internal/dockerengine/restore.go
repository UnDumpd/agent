// Package dockerengine restores dumps into ephemeral database containers.
// Database client programs run inside those containers, not on the agent host.
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
	"path/filepath"
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
	mongoImage        = "mongo:8"
	dbName            = "undump_check"
	pgUser            = "undump"
	readyTimeout      = 60 * time.Second
	containerDumpPath = "/tmp/dump"
	// mongoDumpDirName is the fixed name the source's collection directory
	// is copied under inside the container, regardless of its host name —
	// mongorestore infers the original database name from this directory
	// name, and nsFrom/nsTo below remap it to dbName.
	mongoDumpDirName = "src"
)

// engineSpec holds the engine-specific parts of a restore.
type engineSpec struct {
	image        string
	port         string
	tmpfsPath    string
	containerEnv func(password string) []string
	execEnv      func(password string) []string
	readyCmd     []string
	restoreCmd   func(containerDumpPath string) []string
	queryCmd     func(query string) []string
	dsn          func(password, host, port string) string
}

func specFor(engine Engine) engineSpec {
	switch engine {
	case EngineMySQL:
		return mysqlSpec
	case EngineMongo:
		return mongoSpec
	default:
		return postgresSpec(engine == EnginePostgresCustom)
	}
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
	// mysqladmin also sees the unauthenticated bootstrap server. An
	// authenticated query waits for the real server instead.
	readyCmd: []string{"mysql", "-uroot", "-e", "SELECT 1"},
	restoreCmd: func(path string) []string {
		// mysql has no --file option for plain SQL.
		return []string{"sh", "-c", fmt.Sprintf("mysql -uroot %s < %s", dbName, path)}
	},
	queryCmd: func(query string) []string {
		return []string{"mysql", "-uroot", "-N", "-B", dbName, "-e", query}
	},
	dsn: func(password, host, port string) string {
		return fmt.Sprintf("mysql://root:%s@%s:%s/%s", password, host, port, dbName)
	},
}

// mongoSpec restores a single mongodump collection directory. The ephemeral
// container runs without authentication (no MONGO_INITDB_ROOT_*), matching
// how the Postgres container is queried here — same trust model, both scoped
// to a throwaway container bound to 127.0.0.1 and force-removed on Close.
var mongoSpec = engineSpec{
	image:        mongoImage,
	port:         "27017/tcp",
	tmpfsPath:    "/data/db",
	containerEnv: func(password string) []string { return nil },
	execEnv:      func(password string) []string { return nil },
	readyCmd:     []string{"mongosh", "--quiet", "--eval", "db.adminCommand('ping')"},
	restoreCmd: func(path string) []string {
		return []string{
			"mongorestore",
			"--dir=" + path,
			"--nsInclude=" + mongoDumpDirName + ".*",
			"--nsFrom=" + mongoDumpDirName + ".*",
			"--nsTo=" + dbName + ".*",
		}
	},
	queryCmd: func(query string) []string {
		return []string{"mongosh", "--quiet", dbName, "--eval", query}
	},
	dsn: func(password, host, port string) string {
		return fmt.Sprintf("mongodb://%s:%s/%s", host, port, dbName)
	},
}

// Outcome is the result of a restore attempt.
type Outcome struct {
	OK         bool
	RTOSeconds float64
	Detail     string
}

// Session owns an ephemeral database container. Call Close when finished.
type Session struct {
	Outcome Outcome
	DSN     string

	cli         *dockerclient.Client
	containerID string
	password    string
	spec        engineSpec
	engine      Engine
}

// EngineName reports the engine detected from the dump.
func (s *Session) EngineName() string {
	switch s.engine {
	case EngineMySQL:
		return "mysql"
	case EngineMongo:
		return "mongo"
	default:
		return "postgres"
	}
}

// Restore starts an ephemeral database and restores dumpPath into it.
// Infrastructure failures are returned as errors; a rejected dump is recorded
// in Session.Outcome so callers can report a failed restore.
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
			// PostgreSQL 18 keeps versioned data below /var/lib/postgresql.
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

	hostPort, err := session.waitReady(ctx)
	if err != nil {
		if cerr := session.Close(); cerr != nil {
			slog.Warn("failed to remove container after readiness error", "error", cerr)
		}
		return nil, err
	}
	session.DSN = spec.dsn(password, "127.0.0.1", hostPort)

	ok, detail, err := session.restoreDump(ctx, dumpPath)
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

// QueryScalar runs a query with the database client inside the container.
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

// Close force-removes the container and closes the Docker client.
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

func (s *Session) waitReady(ctx context.Context) (hostPort string, err error) {
	deadline := time.Now().Add(readyTimeout)
	for time.Now().Before(deadline) {
		code, _, execErr := s.exec(ctx, s.spec.readyCmd, s.spec.execEnv(s.password))
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

func (s *Session) restoreDump(ctx context.Context, dumpPath string) (ok bool, detail string, err error) {
	info, err := os.Stat(dumpPath)
	if err != nil {
		return false, "", fmt.Errorf("statting dump: %w", err)
	}
	if info.IsDir() {
		err = s.copyDirToContainer(ctx, dumpPath, containerDumpPath+"/"+mongoDumpDirName)
	} else {
		err = s.copyFileToContainer(ctx, dumpPath, containerDumpPath)
	}
	if err != nil {
		return false, "", fmt.Errorf("copying dump into container: %w", err)
	}

	code, output, err := s.exec(ctx, s.spec.restoreCmd(containerDumpPath), s.spec.execEnv(s.password))
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

func (s *Session) copyFileToContainer(ctx context.Context, hostPath, containerPath string) error {
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

// copyDirToContainer copies the regular files directly inside hostDir (a
// mongodump collection directory: *.bson, *.metadata.json, prelude.json;
// nonrecursive, matching mongodump's flat per-database layout) into
// containerPath inside the container.
func (s *Session) copyDirToContainer(ctx context.Context, hostDir, containerPath string) error {
	entries, err := os.ReadDir(hostDir)
	if err != nil {
		return fmt.Errorf("reading dump directory: %w", err)
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(hostDir, entry.Name()))
		if err != nil {
			return fmt.Errorf("reading %s: %w", entry.Name(), err)
		}
		if err := tw.WriteHeader(&tar.Header{
			Name: containerPath + "/" + entry.Name(),
			Mode: 0644,
			Size: int64(len(data)),
		}); err != nil {
			return err
		}
		if _, err := tw.Write(data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return s.cli.CopyToContainer(ctx, s.containerID, "/", &buf, container.CopyToContainerOptions{})
}

// ensureImage pulls missing images before RTO measurement starts.
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
