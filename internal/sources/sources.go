// Package sources acquires configured backup sources for restore.
package sources

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"undump/internal/config"
	"undump/internal/sources/local"
	"undump/internal/sources/s3"
)

// Artifact is an acquired backup ready for restore and reporting.
type Artifact struct {
	Path      string
	Size      int64
	ReportURI string
}

// Acquire resolves a configured source into a backup artifact.
func Acquire(ctx context.Context, src config.SourceConfig, destDir string) (Artifact, error) {
	switch src.Type {
	case "local":
		path, size, err := local.Resolve(src, time.Now())
		if err != nil {
			return Artifact{}, err
		}
		return Artifact{
			Path:      path,
			Size:      size,
			ReportURI: "local://" + filepath.Base(path),
		}, nil
	case "s3":
		path, size, err := s3.Fetch(ctx, src, destDir)
		if err != nil {
			return Artifact{}, err
		}
		return Artifact{
			Path:      path,
			Size:      size,
			ReportURI: src.URI,
		}, nil
	default:
		return Artifact{}, fmt.Errorf("unsupported source type %q", src.Type)
	}
}

// ReportURI returns the source identity safe to use before acquisition.
func ReportURI(src config.SourceConfig) string {
	if src.Type == "local" {
		return "local://unresolved"
	}
	return src.URI
}
