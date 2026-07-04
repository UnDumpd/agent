# MySQL Backup Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let `undump check` restore-test MySQL (`mysqldump` plain-SQL) backups, in addition to the Postgres dumps it already handles.

**Architecture:** Replace the bool `detectFormat` in `internal/dockerengine/format.go` with a 3-way `detectEngine` (Postgres custom / Postgres plain / MySQL, detected from dump content signatures). Generalize `internal/dockerengine/restore.go` around an internal `engineSpec` table (image, port, container env, exec env, ready check, restore command, DSN builder) so `Restore()` picks the right container/commands for the detected engine instead of the current hardcoded Postgres-only logic.

**Tech Stack:** Go 1.25, Docker Engine API (`github.com/docker/docker/client`), `postgres:18` / `mysql:8` images, testify.

Spec: [`docs/superpowers/specs/2026-07-04-mysql-support-design.md`](../specs/2026-07-04-mysql-support-design.md)

---

### Task 1: MySQL dump fixture

**Files:**
- Create: `testdata/sample_mysql.sql`

- [ ] **Step 1: Create the fixture**

Real `mysqldump` plain-SQL output (headers + one small table), mirroring the existing `testdata/sample_plain.sql` Postgres fixture:

```sql
-- MySQL dump 10.13  Distrib 8.0.35, for Linux (x86_64)
--
-- Host: localhost    Database: undump_check
-- ------------------------------------------------------
-- Server version	8.0.35

/*!40101 SET @OLD_CHARACTER_SET_CLIENT=@@CHARACTER_SET_CLIENT */;
/*!40101 SET @OLD_CHARACTER_SET_RESULTS=@@CHARACTER_SET_RESULTS */;
/*!40101 SET @OLD_COLLATION_CONNECTION=@@COLLATION_CONNECTION */;
/*!50503 SET NAMES utf8mb4 */;
/*!40103 SET @OLD_TIME_ZONE=@@TIME_ZONE */;
/*!40103 SET TIME_ZONE='+00:00' */;
/*!40014 SET @OLD_UNIQUE_CHECKS=@@UNIQUE_CHECKS, UNIQUE_CHECKS=0 */;
/*!40014 SET @OLD_FOREIGN_KEY_CHECKS=@@FOREIGN_KEY_CHECKS, FOREIGN_KEY_CHECKS=0 */;
/*!40101 SET @OLD_SQL_MODE=@@SQL_MODE, SQL_MODE='NO_AUTO_VALUE_ON_ZERO' */;
/*!40111 SET @OLD_SQL_NOTES=@@SQL_NOTES, SQL_NOTES=0 */;

--
-- Table structure for table `widgets`
--

DROP TABLE IF EXISTS `widgets`;
/*!40101 SET @saved_cs_client     = @@character_set_client */;
/*!50503 SET character_set_client = utf8mb4 */;
CREATE TABLE `widgets` (
  `id` int NOT NULL AUTO_INCREMENT,
  `name` varchar(255) NOT NULL,
  `created_at` timestamp NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
/*!40101 SET character_set_client = @saved_cs_client */;

--
-- Dumping data for table `widgets`
--

LOCK TABLES `widgets` WRITE;
/*!40000 ALTER TABLE `widgets` DISABLE KEYS */;
INSERT INTO `widgets` VALUES (1,'alpha','2026-07-01 18:10:17'),(2,'bravo','2026-07-01 18:10:17'),(3,'charlie','2026-07-01 18:10:17');
/*!40000 ALTER TABLE `widgets` ENABLE KEYS */;
UNLOCK TABLES;

/*!40103 SET TIME_ZONE=@OLD_TIME_ZONE */;

/*!40101 SET SQL_MODE=@OLD_SQL_MODE */;
/*!40014 SET FOREIGN_KEY_CHECKS=@OLD_FOREIGN_KEY_CHECKS */;
/*!40014 SET UNIQUE_CHECKS=@OLD_UNIQUE_CHECKS */;
/*!40101 SET CHARACTER_SET_CLIENT=@OLD_CHARACTER_SET_CLIENT */;
/*!40101 SET CHARACTER_SET_RESULTS=@OLD_CHARACTER_SET_RESULTS */;
/*!40101 SET COLLATION_CONNECTION=@OLD_COLLATION_CONNECTION */;
/*!40111 SET SQL_NOTES=@OLD_SQL_NOTES */;

-- Dump completed on 2026-07-04  9:00:00
```

- [ ] **Step 2: Commit**

```bash
git add testdata/sample_mysql.sql
git commit -m "test: add mysqldump fixture for engine detection"
```

---

### Task 2: Engine detection (`detectEngine`)

**Files:**
- Modify: `internal/dockerengine/format.go` (full rewrite)
- Modify: `internal/dockerengine/format_test.go` (full rewrite)

- [ ] **Step 1: Write the failing tests**

Replace the entire contents of `internal/dockerengine/format_test.go`:

```go
package dockerengine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectEngine_CustomDump(t *testing.T) {
	engine, err := detectEngine("../../testdata/sample_custom.dump")
	require.NoError(t, err)
	assert.Equal(t, EnginePostgresCustom, engine)
}

func TestDetectEngine_PlainSQL(t *testing.T) {
	engine, err := detectEngine("../../testdata/sample_plain.sql")
	require.NoError(t, err)
	assert.Equal(t, EnginePostgresPlain, engine)
}

func TestDetectEngine_MySQLDump(t *testing.T) {
	engine, err := detectEngine("../../testdata/sample_mysql.sql")
	require.NoError(t, err)
	assert.Equal(t, EngineMySQL, engine)
}

func TestDetectEngine_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.dump")
	require.NoError(t, os.WriteFile(path, []byte{}, 0644))

	engine, err := detectEngine(path)
	require.NoError(t, err)
	assert.Equal(t, EnginePostgresPlain, engine)
}
```

- [ ] **Step 2: Run tests to verify they fail to compile**

Run: `bash hack/godev.sh test ./internal/dockerengine/... -run TestDetectEngine -v`
Expected: build failure — `detectEngine`, `EnginePostgresCustom`, `EnginePostgresPlain`, `EngineMySQL` are undefined (they only exist in the old `detectFormat`/bool form).

- [ ] **Step 3: Replace `internal/dockerengine/format.go`**

```go
package dockerengine

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

const (
	pgCustomSignature  = "PGDMP"
	mysqlDumpSignature = "-- MySQL dump"
)

// Engine identifies which restore path a dump needs.
type Engine int

const (
	// EnginePostgresPlain is also the fallback for any dump that doesn't
	// match a known signature — psql -f already behaves reasonably (and
	// fails loudly via ON_ERROR_STOP) on unrecognized input.
	EnginePostgresPlain Engine = iota
	EnginePostgresCustom
	EngineMySQL
)

// detectEngine determines which restore path a dump needs by sniffing its
// content:
//   - custom-format pg_dump (-Fc) starts with the "PGDMP" magic bytes
//   - mysqldump plain-SQL output always starts with a "-- MySQL dump" header
//     comment
//   - anything else (including empty/short files) falls back to Postgres
//     plain SQL — psql will fail on it with a clear error instead of us
//     panicking while reading the signature
func detectEngine(dumpPath string) (Engine, error) {
	f, err := os.Open(dumpPath)
	if err != nil {
		return EnginePostgresPlain, fmt.Errorf("opening dump: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Warn("failed to close dump file after engine detection", "path", dumpPath, "error", cerr)
		}
	}()

	sig := make([]byte, len(pgCustomSignature))
	n, err := io.ReadFull(f, sig)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return EnginePostgresPlain, fmt.Errorf("reading dump signature: %w", err)
	}
	if n == len(pgCustomSignature) && string(sig) == pgCustomSignature {
		return EnginePostgresCustom, nil
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return EnginePostgresPlain, fmt.Errorf("seeking dump: %w", err)
	}
	scanner := bufio.NewScanner(f)
	if scanner.Scan() && strings.HasPrefix(scanner.Text(), mysqlDumpSignature) {
		return EngineMySQL, nil
	}

	return EnginePostgresPlain, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `bash hack/godev.sh test ./internal/dockerengine/... -run TestDetectEngine -v`
Expected: PASS (4/4).

- [ ] **Step 5: Commit**

```bash
git add internal/dockerengine/format.go internal/dockerengine/format_test.go
git commit -m "feat: detect MySQL dumps alongside Postgres formats"
```

---

### Task 3: Engine-parameterized restore (`engineSpec`)

**Files:**
- Modify: `internal/dockerengine/restore.go` (full rewrite)
- Modify: `internal/dockerengine/restore_test.go` (add MySQL case)

This task requires Docker (via `hack/godev.sh`, which mounts `/var/run/docker.sock`). Pull the images once before running: `docker pull postgres:18 && docker pull mysql:8`.

- [ ] **Step 1: Add the failing MySQL restore test**

Add this test to the end of `internal/dockerengine/restore_test.go` (keep every existing test in that file unchanged):

```go
func TestRestore_MySQLFormat(t *testing.T) {
	ctx := context.Background()
	session, err := Restore(ctx, "../../testdata/sample_mysql.sql")
	require.NoError(t, err)
	defer func() { assert.NoError(t, session.Close()) }()

	assert.True(t, session.Outcome.OK, session.Outcome.Detail)
	assert.Greater(t, session.Outcome.RTOSeconds, 0.0)
	assert.Contains(t, session.DSN, "mysql://")
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `bash hack/godev.sh test ./internal/dockerengine/... -run TestRestore_MySQLFormat -v`
Expected: FAIL — restore currently always starts `postgres:18` regardless of dump content, so `psql`/`pg_restore` chokes on the MySQL SQL and `Outcome.OK` is `false` (or the DSN never contains `mysql://`).

- [ ] **Step 3: Replace `internal/dockerengine/restore.go`**

```go
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
	// succeeds against that bootstrap instance too (confirmed while
	// implementing this plan). Requiring an authenticated query means
	// readiness only trips once the real password-protected server is up.
	readyCmd: []string{"mysql", "-uroot", "-e", "SELECT 1"},
	restoreCmd: func(path string) []string {
		// mysql has no --file flag for plain SQL; shell redirection is the
		// standard way to feed a dump to the client.
		return []string{"sh", "-c", fmt.Sprintf("mysql -uroot %s < %s", dbName, path)}
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
	spec        engineSpec
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

	session := &Session{cli: cli, containerID: resp.ID, spec: spec}

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
```

Note: `dbUser` is renamed to `pgUser` (it's now Postgres-specific — MySQL uses a separate hardcoded `root` user inline in `mysqlSpec`). No other file references `dbUser`, so this rename is self-contained.

- [ ] **Step 4: Run the full `dockerengine` package tests**

Run: `bash hack/godev.sh test ./internal/dockerengine/... -v`
Expected: PASS — all pre-existing Postgres tests (`TestRestore_CustomFormat`, `TestRestore_PlainSQLFormat`, `TestRestore_BrokenPlainSQLFails`, `TestRestore_ContainerRemovedAfterClose`) and the new `TestRestore_MySQLFormat`.

- [ ] **Step 5: Commit**

```bash
git add internal/dockerengine/restore.go internal/dockerengine/restore_test.go
git commit -m "feat: restore MySQL dumps via a mysql:8 container"
```

---

### Task 4: Update configuration docs

**Files:**
- Modify: `CONFIGURATION.md:86`
- Modify: `CONFIGURATION.md:117`
- Modify: `CONFIGURATION.md:119-129`

- [ ] **Step 1: Update the `engine` field description**

In `CONFIGURATION.md`, replace line 86:

```
| `engine` | string | yes | Database engine. Only `postgres` is supported today. |
```

with:

```
| `engine` | string | yes | Reporting label only — the restore path is auto-detected from the dump's content, not from this field. See "The restore environment" below. |
```

- [ ] **Step 2: Update the "live Postgres" mention**

Replace line 117:

```
> **Current status (v0.1.0):** these three check types are parsed and validated, but **not executed yet** — the agent logs that they'll arrive in a future phase. The one check that always runs is `restore` itself: did the dump actually restore into a live Postgres without errors? You don't declare it; it's implicit for every target. Keep the checks in your config — they'll light up when the corresponding agent version ships.
```

with:

```
> **Current status (v0.1.0):** these three check types are parsed and validated, but **not executed yet** — the agent logs that they'll arrive in a future phase. The one check that always runs is `restore` itself: did the dump actually restore into a live database (Postgres or MySQL) without errors? You don't declare it; it's implicit for every target. Keep the checks in your config — they'll light up when the corresponding agent version ships.
```

- [ ] **Step 3: Rewrite "The restore environment" section**

Replace lines 119-129 (from `## The restore environment` through the container-removal bullet):

```
## The restore environment

Not configurable today, but worth knowing what happens on your Docker host for each target:

- The agent talks to Docker via the standard environment (`DOCKER_HOST` etc., or the mounted `/var/run/docker.sock` when running in the published image).
- It starts a **`postgres:18`** container with a random one-shot password, database `undump_check`, storage on `tmpfs` (nothing touches disk), and port 5432 published on `127.0.0.1` only, on a random host port.
- The image must already be present on the host — the agent does **not** pull it. Run `docker pull postgres:18` once when provisioning.
- Readiness is waited for up to **60 seconds**, then the run errors.
- Dump format is detected automatically: custom-format dumps go through `pg_restore --no-owner --no-acl`, plain-SQL dumps through `psql --set ON_ERROR_STOP=1` (without which psql happily exits 0 on broken SQL).
- `pg_restore`/`psql` run **inside** the container via docker exec — the agent host needs no Postgres client tools.
- The container is force-removed when the target finishes, **including on failure and on infrastructure errors**.
```

with:

```
## The restore environment

Not configurable today, but worth knowing what happens on your Docker host for each target:

- The agent talks to Docker via the standard environment (`DOCKER_HOST` etc., or the mounted `/var/run/docker.sock` when running in the published image).
- Which database engine gets spun up is **auto-detected from the dump's content**, not from `targets[].engine` — that field is reporting-only today. Custom-format `pg_dump` (`PGDMP` magic bytes) and plain-SQL dumps (Postgres, or the default fallback for anything unrecognized) start a **`postgres:18`** container; dumps starting with the `-- MySQL dump` header that `mysqldump` emits start a **`mysql:8`** container instead.
- Each container gets a random one-shot password, database `undump_check`, storage on `tmpfs` (nothing touches disk), and its default port (5432 for Postgres, 3306 for MySQL) published on `127.0.0.1` only, on a random host port.
- Both images must already be present on the host — the agent does **not** pull them. Run `docker pull postgres:18` and `docker pull mysql:8` once when provisioning.
- Readiness is waited for up to **60 seconds**, then the run errors.
- Postgres dump format is detected automatically within the Postgres path too: custom-format dumps go through `pg_restore --no-owner --no-acl`, plain-SQL dumps through `psql --set ON_ERROR_STOP=1` (without which psql happily exits 0 on broken SQL). MySQL support is currently **`mysqldump` plain SQL only** (no `.sql.gz`, no xtrabackup/physical backups) and restores via `mysql -uroot <db> < dump`.
- Restore clients run **inside** the container via docker exec — the agent host needs no Postgres or MySQL client tools.
- The container is force-removed when the target finishes, **including on failure and on infrastructure errors**.
```

- [ ] **Step 4: Commit**

```bash
git add CONFIGURATION.md
git commit -m "docs: describe MySQL restore support"
```

---

### Task 5: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Run the full test suite**

Run: `bash hack/godev.sh test ./... -v`
Expected: PASS across all packages, including the `dockerengine` Postgres and MySQL restore tests.

- [ ] **Step 2: Run the linter**

Run: `bash hack/godev.sh run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run ./...`
Expected: no issues.

- [ ] **Step 3: Confirm nothing left uncommitted**

Run: `git status`
Expected: `nothing to commit, working tree clean`

---

## Out of scope (per spec)

- Compressed dumps (`.sql.gz`).
- Physical/binary backup formats (xtrabackup, mysqlbackup).
- Wiring `config.Target.Engine` into the detection path.
- Rowcount/freshness/sql_assert checks for MySQL (blocked on the same cross-engine TODO already blocking them for Postgres in `cmd/undump/main.go`).
