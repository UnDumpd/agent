package s3_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscreds "github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"undump/internal/config"
	"undump/internal/sources/s3"
)

// Fixed dev credentials for the local docker-compose S3-compatible service
// (see .env.example, docker-compose.yml). Not production secrets.
func testSource(uri string) config.SourceConfig {
	return config.SourceConfig{
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

func TestFetch_MongoDumpPrefixDownloadsAllDirectChildren(t *testing.T) {
	dest := t.TempDir()
	src := testSource("s3://undump-test/dumps/mongo/")
	src.MinAge = config.Duration{Duration: 0, Set: true}

	path, _, err := s3.Fetch(context.Background(), src, dest)
	require.NoError(t, err)
	assert.Equal(t, dest, path)

	entries, err := os.ReadDir(dest)
	require.NoError(t, err)

	names := make(map[string]bool, len(entries))
	for _, entry := range entries {
		require.False(t, entry.IsDir(), "unexpected directory entry %q", entry.Name())
		names[entry.Name()] = true
	}
	assert.True(t, names["widgets.bson"])
	assert.True(t, names["widgets.metadata.json"])
	assert.True(t, names["prelude.json"])
}

func TestFetch_MongoDumpPrefixRejectsPattern(t *testing.T) {
	dest := t.TempDir()
	src := testSource("s3://undump-test/dumps/mongo/")
	src.Pattern = "*.bson"
	src.MinAge = config.Duration{Duration: 0, Set: true}

	_, _, err := s3.Fetch(context.Background(), src, dest)
	require.Error(t, err)
	assert.Equal(t, "s3 source: pattern is invalid on a mongodump prefix", err.Error())
}

func TestFetch_MongoDumpPrefixTooYoungFails(t *testing.T) {
	prefix := fmt.Sprintf("dumps/mongo-fresh-%s-%d/", strings.ReplaceAll(t.Name(), "/", "-"), time.Now().UnixNano())
	uploadTestObject(t, prefix+"widgets.metadata.json", "{}")
	uploadTestObject(t, prefix+"widgets.bson", "x")

	dest := t.TempDir()
	src := testSource("s3://undump-test/" + prefix)
	src.MinAge = config.Duration{Duration: time.Hour, Set: true}

	_, _, err := s3.Fetch(context.Background(), src, dest)
	require.Error(t, err)
	assert.Equal(t, "s3 source: mongodump prefix is younger than min_age", err.Error())
}

func uploadTestObject(t *testing.T, key, body string) {
	t.Helper()
	cli := awss3.New(awss3.Options{
		Region:       "us-east-1",
		Credentials:  awscreds.NewStaticCredentialsProvider("minioadmin", "minioadmin", ""),
		UsePathStyle: true,
		BaseEndpoint: aws.String("http://minio:9000"),
	})
	bucket := "undump-test"
	_, err := cli.PutObject(context.Background(), &awss3.PutObjectInput{
		Bucket: &bucket,
		Key:    &key,
		Body:   strings.NewReader(body),
	})
	require.NoError(t, err)
}
