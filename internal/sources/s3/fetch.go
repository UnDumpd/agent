// Package s3 downloads dumps from S3-compatible storage.
package s3

import (
	"context"
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

	"undump/internal/config"
)

// Fetch downloads one dump into destDir. Prefix URIs select the newest object,
// optionally filtered by Pattern.
func Fetch(ctx context.Context, src config.S3Source, destDir string) (path string, size int64, err error) {
	bucket, key, err := parseURI(src.URI)
	if err != nil {
		return "", 0, err
	}

	cli := newClient(src)

	if key == "" || strings.HasSuffix(src.URI, "/") {
		key, err = latestKeyByPrefix(ctx, cli, bucket, key, src.Pattern)
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

func newClient(src config.S3Source) *awss3.Client {
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

func latestKeyByPrefix(ctx context.Context, cli *awss3.Client, bucket, prefix, pattern string) (string, error) {
	out, err := cli.ListObjectsV2(ctx, &awss3.ListObjectsV2Input{Bucket: &bucket, Prefix: &prefix})
	if err != nil {
		return "", fmt.Errorf("listing objects with prefix %q: %w", prefix, err)
	}

	var latestKey string
	var latestTime time.Time
	found := false
	for _, obj := range out.Contents {
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
