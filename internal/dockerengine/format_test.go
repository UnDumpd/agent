package dockerengine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectEngine(t *testing.T) {
	tests := []struct {
		name string
		path string
		want Engine
	}{
		{"pg custom", "../../testdata/sample_custom.dump", EnginePostgresCustom},
		{"pg plain", "../../testdata/sample_plain.sql", EnginePostgresPlain},
		{"mysql", "../../testdata/sample_mysql.sql", EngineMySQL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine, err := detectEngine(tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, engine)
		})
	}
}

func TestEngineNameMapsDetectedEngine(t *testing.T) {
	cases := []struct {
		engine Engine
		want   string
	}{
		{EnginePostgresPlain, "postgres"},
		{EnginePostgresCustom, "postgres"},
		{EngineMySQL, "mysql"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, (&Session{engine: tc.engine}).EngineName())
	}
}

func TestDetectEngine_EmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.dump")
	require.NoError(t, os.WriteFile(path, []byte{}, 0644))

	engine, err := detectEngine(path)
	require.NoError(t, err)
	assert.Equal(t, EnginePostgresPlain, engine)
}
