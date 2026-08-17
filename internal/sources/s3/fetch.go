// Package s3 downloads dumps from S3-compatible storage.
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscreds "github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"undump/internal/config"
	"undump/internal/dockerengine"
)

var (
	errPatternOnMongoDump = errors.New("s3 source: pattern is invalid on a mongodump prefix")
	errMongoDumpTooYoung  = errors.New("s3 source: mongodump prefix is younger than min_age")
)

// Fetch downloads one dump into destDir and returns its path. A prefix URI
// normally resolves to the newest object under it, optionally filtered by
// Pattern; a prefix holding a mongodump collection is pulled whole instead,
// and the returned path is destDir itself.
func Fetch(ctx context.Context, src config.SourceConfig, destDir string) (path string, size int64, err error) {
	bucket, key, err := parseURI(src.URI)
	if err != nil {
		return "", 0, err
	}

	cli := newClient(src)

	if key == "" || strings.HasSuffix(src.URI, "/") {
		objects, err := listObjects(ctx, cli, bucket, key)
		if err != nil {
			return "", 0, err
		}

		children := directChildren(objects, key)
		if isMongoDumpKeys(children) {
			return fetchMongoDir(ctx, cli, bucket, children, src, destDir)
		}

		key, err = latestKey(objects, key, bucket, src.Pattern)
		if err != nil {
			return "", 0, err
		}
	}

	dest := filepath.Join(destDir, filepath.Base(key))
	if err := downloadTo(ctx, cli, bucket, key, dest); err != nil {
		return "", 0, err
	}

	info, err := os.Stat(dest)
	if err != nil {
		return "", 0, fmt.Errorf("stat downloaded file: %w", err)
	}
	return dest, info.Size(), nil
}

func newClient(src config.SourceConfig) *awss3.Client {
	opts := awss3.Options{
		Region:       "us-east-1",
		Credentials:  awscreds.NewStaticCredentialsProvider(src.AccessKey, src.SecretKey, ""),
		UsePathStyle: true, // required for non-AWS S3-compatible endpoints
	}
	if src.Region != "" {
		opts.Region = src.Region
	}
	if src.EndpointURL != "" {
		opts.BaseEndpoint = aws.String(src.EndpointURL)
	}
	return awss3.New(opts)
}

func parseURI(uri string) (bucket, key string, err error) {
	const prefix = "s3://"
	if !strings.HasPrefix(uri, prefix) {
		return "", "", fmt.Errorf("expected an s3:// URI, got %q", uri)
	}
	rest := strings.TrimPrefix(uri, prefix)
	bucket, key, _ = strings.Cut(rest, "/")
	return bucket, key, nil
}

func listObjects(ctx context.Context, cli *awss3.Client, bucket, prefix string) ([]types.Object, error) {
	out, err := cli.ListObjectsV2(ctx, &awss3.ListObjectsV2Input{Bucket: &bucket, Prefix: &prefix})
	if err != nil {
		return nil, fmt.Errorf("listing objects with prefix %q: %w", prefix, err)
	}
	return out.Contents, nil
}

// latestKey picks the newest object under prefix, optionally narrowed by
// pattern. The scan deliberately includes nested keys — rotated dumps often
// sit in dated subfolders, and restricting this to direct children would hide
// them.
func latestKey(objects []types.Object, prefix, bucket, pattern string) (string, error) {
	var latestKey string
	var latestTime time.Time
	found := false
	for _, obj := range objects {
		if obj.Key == nil || obj.LastModified == nil {
			continue
		}
		if pattern != "" {
			matched, err := filepath.Match(pattern, filepath.Base(*obj.Key))
			if err != nil {
				return "", fmt.Errorf("matching pattern %q: %w", pattern, err)
			}
			if !matched {
				continue
			}
		}
		if !found || obj.LastModified.After(latestTime) {
			latestKey = *obj.Key
			latestTime = *obj.LastModified
			found = true
		}
	}

	if !found {
		if pattern != "" {
			return "", fmt.Errorf("no objects matching pattern %q with prefix %q in bucket %q", pattern, prefix, bucket)
		}
		return "", fmt.Errorf("no objects with prefix %q in bucket %q", prefix, bucket)
	}
	return latestKey, nil
}

// directChildren drops anything nested deeper than prefix. A mongodump
// collection directory is flat, so nested keys mean the prefix holds
// something else — rotated dumps, several databases — and not one dump.
func directChildren(objects []types.Object, prefix string) []types.Object {
	var children []types.Object
	for _, obj := range objects {
		if obj.Key == nil {
			continue
		}
		rest := strings.TrimPrefix(*obj.Key, prefix)
		if rest == "" || strings.Contains(rest, "/") {
			continue
		}
		children = append(children, obj)
	}
	return children
}

// isMongoDumpKeys runs the same signature check local sources use, over
// object basenames instead of directory entries.
func isMongoDumpKeys(objects []types.Object) bool {
	names := make([]string, 0, len(objects))
	for _, obj := range objects {
		if obj.Key == nil {
			continue
		}
		names = append(names, filepath.Base(*obj.Key))
	}
	return dockerengine.IsMongoDumpFileSet(names)
}

// fetchMongoDir pulls a whole mongodump prefix into destDir. The prefix is
// the dump artifact, so there is nothing to select inside it and pattern is
// rejected rather than quietly ignored.
func fetchMongoDir(ctx context.Context, cli *awss3.Client, bucket string, objects []types.Object, src config.SourceConfig, destDir string) (string, int64, error) {
	if src.Pattern != "" {
		return "", 0, errPatternOnMongoDump
	}

	// Unlike a single-object fetch, pulling a prefix is not atomic: a listing
	// can catch a backup job halfway through uploading and mix fresh
	// collections with stale ones. Hence min_age off the newest object.
	var latest time.Time
	for _, obj := range objects {
		if obj.LastModified == nil {
			continue
		}
		if obj.LastModified.After(latest) {
			latest = *obj.LastModified
		}
	}
	if !isOldEnough(latest, time.Now(), src.MinAge.Duration) {
		return "", 0, errMongoDumpTooYoung
	}

	// A failure mid-loop leaves partial files behind; the caller runs every S3
	// fetch in a temp directory it removes afterwards.
	var total int64
	for _, obj := range objects {
		if obj.Key == nil {
			continue
		}
		dest := filepath.Join(destDir, filepath.Base(*obj.Key))
		if err := downloadTo(ctx, cli, bucket, *obj.Key, dest); err != nil {
			return "", 0, err
		}
		info, err := os.Stat(dest)
		if err != nil {
			return "", 0, fmt.Errorf("stat downloaded file: %w", err)
		}
		total += info.Size()
	}

	return destDir, total, nil
}

// isOldEnough mirrors the helper of the same name in internal/sources/local,
// taking a plain time.Time because S3 objects have no os.FileInfo. Boundary
// is inclusive, same as there.
func isOldEnough(latest, now time.Time, minAge time.Duration) bool {
	return !latest.After(now.Add(-minAge))
}

func downloadTo(ctx context.Context, cli *awss3.Client, bucket, key, dest string) error {
	out, err := cli.GetObject(ctx, &awss3.GetObjectInput{Bucket: &bucket, Key: &key})
	if err != nil {
		return fmt.Errorf("downloading s3://%s/%s: %w", bucket, key, err)
	}
	defer func() {
		if cerr := out.Body.Close(); cerr != nil {
			slog.Warn("failed to close S3 response body", "bucket", bucket, "key", key, "error", cerr)
		}
	}()

	f, err := os.Create(dest)
	if err != nil {
		return err
	}

	if _, err := io.Copy(f, out.Body); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing file %s: %w", dest, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing file %s: %w", dest, err)
	}
	return nil
}
