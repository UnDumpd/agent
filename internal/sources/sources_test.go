package sources_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"undump/internal/config"
	"undump/internal/sources"
)

func TestReportURI_S3UsesConfiguredURI(t *testing.T) {
	src := config.SourceConfig{
		Type: "s3",
		URI:  "s3://backups/latest.dump",
	}

	assert.Equal(t, src.URI, sources.ReportURI(src))
}

func TestReportURI_LocalIsUnresolvedBeforeAcquisition(t *testing.T) {
	src := config.SourceConfig{
		Type: "local",
		Path: `C:\private\backups`,
	}

	assert.Equal(t, "local://unresolved", sources.ReportURI(src))
}

func TestAcquire_RejectsUnsupportedSourceType(t *testing.T) {
	_, err := sources.Acquire(
		context.Background(),
		config.SourceConfig{Type: "ftp"},
		t.TempDir(),
	)

	require.EqualError(t, err, `unsupported source type "ftp"`)
}

func TestAcquire_S3ReturnsArtifact(t *testing.T) {
	src := config.SourceConfig{
		Type:        "s3",
		URI:         "s3://undump-test/dumps/exact.dump",
		EndpointURL: "http://minio:9000",
		AccessKey:   "minioadmin",
		SecretKey:   "minioadmin",
	}

	artifact, err := sources.Acquire(context.Background(), src, t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, int64(2801), artifact.Size)
	assert.Equal(t, src.URI, artifact.ReportURI)

	data, err := os.ReadFile(artifact.Path)
	require.NoError(t, err)
	assert.Equal(t, "PGDMP", string(data[:5]))
}

func TestAcquire_LocalReturnsOriginalFileWithoutCopying(t *testing.T) {
	sourceDir := t.TempDir()
	path := filepath.Join(sourceDir, "backup.dump")
	require.NoError(t, os.WriteFile(path, []byte("local dump"), 0o600))
	destDir := t.TempDir()

	artifact, err := sources.Acquire(context.Background(), config.SourceConfig{
		Type: "local",
		Path: path,
	}, destDir)

	require.NoError(t, err)
	assert.Equal(t, path, artifact.Path)
	assert.Equal(t, int64(10), artifact.Size)
	assert.Equal(t, "local://backup.dump", artifact.ReportURI)

	entries, err := os.ReadDir(destDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestAcquire_LocalFailureDoesNotRevealConfiguredPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.dump")

	_, err := sources.Acquire(context.Background(), config.SourceConfig{
		Type: "local",
		Path: path,
	}, t.TempDir())

	require.EqualError(t, err, "local source: configured path is unavailable")
	assert.NotContains(t, err.Error(), path)
	assert.NotContains(t, err.Error(), filepath.Dir(path))
}
