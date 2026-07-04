package dockerengine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Requires an available Docker daemon (via /var/run/docker.sock) — when run
// through hack/godev.sh this is guaranteed to be mounted.

func TestRestore_CustomFormat(t *testing.T) {
	ctx := context.Background()
	session, err := Restore(ctx, "../../testdata/sample_custom.dump")
	require.NoError(t, err)
	defer func() { assert.NoError(t, session.Close()) }()

	assert.True(t, session.Outcome.OK, session.Outcome.Detail)
	assert.Greater(t, session.Outcome.RTOSeconds, 0.0)
	assert.NotEmpty(t, session.DSN)
}

func TestRestore_PlainSQLFormat(t *testing.T) {
	ctx := context.Background()
	session, err := Restore(ctx, "../../testdata/sample_plain.sql")
	require.NoError(t, err)
	defer func() { assert.NoError(t, session.Close()) }()

	assert.True(t, session.Outcome.OK, session.Outcome.Detail)
}

func TestRestore_BrokenPlainSQLFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.sql")
	require.NoError(t, os.WriteFile(path, []byte("totally not sql garbage;;\n"), 0644))

	ctx := context.Background()
	session, err := Restore(ctx, path)
	require.NoError(t, err, "the session itself comes up fine — the error is inside Outcome, not err")
	defer func() { assert.NoError(t, session.Close()) }()

	assert.False(t, session.Outcome.OK, "broken SQL should fail the restore, not silently pass")
	assert.Contains(t, session.Outcome.Detail, "rc=")
}

func TestRestore_MySQLFormat(t *testing.T) {
	ctx := context.Background()
	session, err := Restore(ctx, "../../testdata/sample_mysql.sql")
	require.NoError(t, err)
	defer func() { assert.NoError(t, session.Close()) }()

	assert.True(t, session.Outcome.OK, session.Outcome.Detail)
	assert.Greater(t, session.Outcome.RTOSeconds, 0.0)
	assert.Contains(t, session.DSN, "mysql://")
}

func TestRestore_ContainerRemovedAfterClose(t *testing.T) {
	ctx := context.Background()
	session, err := Restore(ctx, "../../testdata/sample_custom.dump")
	require.NoError(t, err)

	containerID := session.containerID
	require.NoError(t, session.Close())

	_, err = session.cli.ContainerInspect(ctx, containerID)
	assert.Error(t, err, "container should be removed after Close()")
}
