package s3_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"undump/internal/config"
	"undump/internal/sources/s3"
)

// Fixed dev credentials for the local docker-compose S3-compatible service
// (see .env.example, docker-compose.yml). Not production secrets.
func testSource(uri string) config.S3Source {
	return config.S3Source{
		Type:        "s3",
		URI:         uri,
		EndpointURL: "http://minio:9000",
		AccessKey:   "minioadmin",
		SecretKey:   "minioadmin",
	}
}

func TestFetch_ExactKey(t *testing.T) {
	dest := t.TempDir()
	path, size, err := s3.Fetch(context.Background(), testSource("s3://undump-test/dumps/exact.dump"), dest)
	require.NoError(t, err)
	assert.Equal(t, int64(2801), size)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "PGDMP", string(data[:5]))
}

func TestFetch_LatestObjectByPrefix(t *testing.T) {
	dest := t.TempDir()
	path, _, err := s3.Fetch(context.Background(), testSource("s3://undump-test/dumps/prefix/"), dest)
	require.NoError(t, err)
	assert.Equal(t, "2026-07-01T00-00-00.dump", filepath.Base(path))
}

func TestFetch_LatestObjectByPrefixWithPattern(t *testing.T) {
	dest := t.TempDir()
	src := testSource("s3://undump-test/dumps/patterned/")
	src.Pattern = "*.dump"

	path, _, err := s3.Fetch(context.Background(), src, dest)
	require.NoError(t, err)
	assert.Equal(t, "2026-07-01T00-00-00.dump", filepath.Base(path))
}

func TestFetch_PatternNoMatchFails(t *testing.T) {
	dest := t.TempDir()
	src := testSource("s3://undump-test/dumps/patterned/")
	src.Pattern = "*.backup"

	_, _, err := s3.Fetch(context.Background(), src, dest)
	assert.Error(t, err)
}

func TestFetch_UnknownKeyFails(t *testing.T) {
	dest := t.TempDir()
	_, _, err := s3.Fetch(context.Background(), testSource("s3://undump-test/dumps/does-not-exist.dump"), dest)
	assert.Error(t, err)
}
