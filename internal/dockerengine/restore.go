// Package dockerengine spins up an ephemeral Postgres container, restores a
// dump into it, and guarantees the container is removed after use.
//
// pg_restore/psql run INSIDE the container via the Docker exec API — not on
// the agent host. This removes the agent's dependency on postgresql-client:
// the required binaries already ship in the postgres:16 image itself.
package dockerengine

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

const (
	postgresImage     = "postgres:18"
	dbName            = "undump_check"
	dbUser            = "undump"
	readyTimeout      = 60 * time.Second
	containerDumpPath = "/tmp/dump"
)

// Outcome — result of a restore attempt.
type Outcome struct {
	OK         bool
	RTOSeconds float64
	Detail     string
}

// Session — an ephemeral Postgres with the dump restored (or not — see Outcome) into it.
// The caller MUST call Close(), ideally via defer right after Restore.
type Session struct {
	Outcome Outcome
	DSN     string

	cli         *dockerclient.Client
	containerID string
}

// Restore spins up a throwaway postgres:16, restores dumpPath into it, and
// returns a Session. An error is returned ONLY if the infrastructure itself
// could not be brought up/reached (Docker unavailable, readiness timeout, etc.) —
// if the restore of the dump itself fails, that's reflected in Session.Outcome.OK=false,
// not in err, so the caller can still build a RunReport with status "fail".
func Restore(ctx context.Context, dumpPath string) (*Session, error) {
	cli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("connecting to Docker: %w", err)
	}

	started := time.Now()
	password := randomPassword()

	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image: postgresImage,
			Env: []string{
				"POSTGRES_PASSWORD=" + password,
				"POSTGRES_USER=" + dbUser,
				"POSTGRES_DB=" + dbName,
			},
			Labels: map[string]string{"app": "undump", "ephemeral": "true"},
		},
		&container.HostConfig{
			// postgres:18+ images refuse to start with anything mounted at the old
			// /var/lib/postgresql/data path — they expect PGDATA to live in a
			// major-version-specific subdirectory under /var/lib/postgresql itself.
			Tmpfs: map[string]string{"/var/lib/postgresql": ""},
			PortBindings: nat.PortMap{
				"5432/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: ""}},
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

	session := &Session{cli: cli, containerID: resp.ID}

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
	session.DSN = fmt.Sprintf("postgresql://%s:%s@127.0.0.1:%s/%s", dbUser, password, hostPort, dbName)

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

func (s *Session) waitReady(ctx context.Context) (hostPort string, err error) {
	deadline := time.Now().Add(readyTimeout)
	for time.Now().Before(deadline) {
		code, _, execErr := s.exec(ctx, []string{"pg_isready", "-U", dbUser, "-d", dbName})
		if execErr == nil && code == 0 {
			inspect, inspectErr := s.cli.ContainerInspect(ctx, s.containerID)
			if inspectErr != nil {
				return "", fmt.Errorf("inspecting container: %w", inspectErr)
			}
			bindings := inspect.NetworkSettings.Ports["5432/tcp"]
			if len(bindings) == 0 {
				return "", fmt.Errorf("port 5432/tcp not published")
			}
			return bindings[0].HostPort, nil
		}
		time.Sleep(time.Second)
	}
	return "", fmt.Errorf("postgres did not become ready within %s", readyTimeout)
}

func (s *Session) restoreDump(ctx context.Context, dumpPath string) (ok bool, detail string, err error) {
	isCustom, err := detectFormat(dumpPath)
	if err != nil {
		return false, "", err
	}
	if err := s.copyToContainer(ctx, dumpPath, containerDumpPath); err != nil {
		return false, "", fmt.Errorf("copying dump into container: %w", err)
	}

	var cmd []string
	if isCustom {
		cmd = []string{"pg_restore", "--no-owner", "--no-acl", "-U", dbUser, "-d", dbName, containerDumpPath}
	} else {
		// --set ON_ERROR_STOP=1 is REQUIRED: without it psql returns exit code 0
		// even on a SQL syntax error (verified manually) — a broken plain-SQL
		// dump would silently look like a successful restore.
		cmd = []string{"psql", "-U", dbUser, "-d", dbName, "--set", "ON_ERROR_STOP=1", "-f", containerDumpPath}
	}

	code, output, err := s.exec(ctx, cmd)
	if err != nil {
		return false, "", fmt.Errorf("restoring: %w", err)
	}
	if code != 0 {
		return false, fmt.Sprintf("restore rc=%d: %s", code, truncate(output, 500)), nil
	}
	return true, "restore completed without errors", nil
}

func (s *Session) exec(ctx context.Context, cmd []string) (exitCode int, output string, err error) {
	execCfg := container.ExecOptions{Cmd: cmd, AttachStdout: true, AttachStderr: true}
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
