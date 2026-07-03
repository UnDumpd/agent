package dockerengine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectFormat_CustomDump(t *testing.T) {
	isCustom, err := detectFormat("../../testdata/sample_custom.dump")
	require.NoError(t, err)
	assert.True(t, isCustom)
}

func TestDetectFormat_PlainSQL(t *testing.T) {
	isCustom, err := detectFormat("../../testdata/sample_plain.sql")
	require.NoError(t, err)
	assert.False(t, isCustom)
}

func TestDetectFormat_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.dump")
	require.NoError(t, os.WriteFile(path, []byte{}, 0644))

	isCustom, err := detectFormat(path)
	require.NoError(t, err)
	assert.False(t, isCustom)
}
