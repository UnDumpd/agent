// Package s3 downloads a dump from S3 into a local temporary directory.
//
// Read-only. The dump's contents never leave the agent's machine — this
// package only places the file on disk; dockerengine takes it from there.
package s3

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscreds "github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"undump/internal/config"
)

// Fetch downloads the dump at src.URI into destDir. If the URI points at a
// "prefix" (ends with "/" or has no key), the object with the most recent
// LastModified is picked; if src.Pattern is set, only objects whose basename
// matches the glob are considered. Returns the downloaded file's path and its
// size in bytes.
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

	candidates := out.Contents
	if pattern != "" {
		candidates = nil
		for _, obj := range out.Contents {
			matched, err := filepath.Match(pattern, filepath.Base(*obj.Key))
			if err != nil {
				return "", fmt.Errorf("matching pattern %q: %w", pattern, err)
			}
			if matched {
				candidates = append(candidates, obj)
			}
		}
	}

	if len(candidates) == 0 {
		if pattern != "" {
			return "", fmt.Errorf("no objects matching pattern %q with prefix %q in bucket %q", pattern, prefix, bucket)
		}
		return "", fmt.Errorf("no objects with prefix %q in bucket %q", prefix, bucket)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].LastModified.After(*candidates[j].LastModified)
	})
	return *candidates[0].Key, nil
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

	if _, err := f.ReadFrom(out.Body); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing file %s: %w", dest, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing file %s: %w", dest, err)
	}
	return nil
}
