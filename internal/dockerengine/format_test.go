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
		{"mongo", "../../testdata/sample_mongo_dump", EngineMongo},
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
		{EngineMongo, "mongo"},
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

func TestDetectEngine_UnrecognizedDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0644))

	_, err := detectEngine(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unrecognized dump directory")
}

func TestIsMongoDumpFileSet(t *testing.T) {
	tests := []struct {
		name  string
		names []string
		want  bool
	}{
		{"nil slice", nil, false},
		{"empty slice", []string{}, false},
		{"unrelated names only", []string{"notes.txt", "README.md"}, false},
		{"orphan metadata file only", []string{"widgets.metadata.json"}, false},
		{"orphan bson file only", []string{"widgets.bson"}, false},
		{"matched pair", []string{"widgets.metadata.json", "widgets.bson"}, true},
		{"matched pair plus unrelated file", []string{"widgets.metadata.json", "widgets.bson", "prelude.json"}, true},
		{"two collections, only one paired", []string{"orders.metadata.json", "widgets.metadata.json", "widgets.bson"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsMongoDumpFileSet(tt.names))
		})
	}
}

func TestIsMongoDumpDir(t *testing.T) {
	assert.True(t, IsMongoDumpDir("../../testdata/sample_mongo_dump"))

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0644))
	assert.False(t, IsMongoDumpDir(dir))

	// An orphan metadata file with no matching <base>.bson (an incomplete or
	// aborted dump) must NOT be treated as a mongodump directory.
	orphan := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(orphan, "widgets.metadata.json"), []byte("{}"), 0644))
	assert.False(t, IsMongoDumpDir(orphan))

	// A complete pair (metadata + matching bson) is recognized.
	paired := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(paired, "widgets.metadata.json"), []byte("{}"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(paired, "widgets.bson"), []byte("\x00"), 0644))
	assert.True(t, IsMongoDumpDir(paired))
}
