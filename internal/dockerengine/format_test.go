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
