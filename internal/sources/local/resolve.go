// Package local selects backup files already present on the local filesystem.
package local

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"undump/internal/config"
	"undump/internal/dockerengine"
)

var (
	errPathUnavailable      = errors.New("local source: configured path is unavailable")
	errNotFileOrDirectory   = errors.New("local source: configured path is not a regular file or directory")
	errPatternOnFile        = errors.New("local source: pattern requires a directory")
	errPatternOnMongoDump   = errors.New("local source: pattern is invalid on a mongodump collection directory")
	errFileTooYoung         = errors.New("local source: configured file is younger than min_age")
	errInvalidPattern       = errors.New("local source: pattern is invalid")
	errDirectoryRead        = errors.New("local source: configured directory cannot be read")
	errCandidateStat        = errors.New("local source: candidate metadata cannot be read")
	errNoCandidates         = errors.New("local source: no eligible backup files")
	errSelectedRead         = errors.New("local source: selected backup file cannot be read")
	errSelectedStat         = errors.New("local source: selected backup file cannot be inspected")
	errSelectedNotRegular   = errors.New("local source: selected backup file is no longer a regular file")
	errSelectedTooYoung     = errors.New("local source: selected backup file is younger than min_age")
	errSelectedChanged      = errors.New("local source: selected backup file changed during acquisition")
	errMongoDumpDirTooYoung = errors.New("local source: configured mongodump directory is younger than min_age")
	errMongoDumpDirStat     = errors.New("local source: mongodump directory contents cannot be inspected")
)

// Resolve selects an eligible local backup at the supplied reference time.
func Resolve(src config.SourceConfig, now time.Time) (path string, size int64, err error) {
	info, err := os.Lstat(src.Path)
	if err != nil {
		return "", 0, errPathUnavailable
	}
	if info.IsDir() {
		// A mongodump collection directory (*.bson + *.metadata.json as
		// direct children) is itself the dump artifact, not a folder of
		// candidate dump files to pick one from.
		if dockerengine.IsMongoDumpDir(src.Path) {
			return resolveMongoDumpDir(src, now)
		}
		return resolveDirectory(src, now)
	}
	return resolveExactFile(src, info, now)
}

func resolveMongoDumpDir(src config.SourceConfig, now time.Time) (string, int64, error) {
	if src.Pattern != "" {
		return "", 0, errPatternOnMongoDump
	}
	entries, err := os.ReadDir(src.Path)
	if err != nil {
		return "", 0, errMongoDumpDirStat
	}

	var size int64
	var newest os.FileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return "", 0, errMongoDumpDirStat
		}
		if !info.Mode().IsRegular() {
			continue
		}
		size += info.Size()
		if newest == nil || info.ModTime().After(newest.ModTime()) {
			newest = info
		}
	}
	if newest == nil {
		return "", 0, errMongoDumpDirStat
	}
	if !isOldEnough(newest, now, src.MinAge.Duration) {
		return "", 0, errMongoDumpDirTooYoung
	}
	return src.Path, size, nil
}

func resolveExactFile(src config.SourceConfig, info os.FileInfo, now time.Time) (string, int64, error) {
	if !info.Mode().IsRegular() {
		return "", 0, errNotFileOrDirectory
	}
	if src.Pattern != "" {
		return "", 0, errPatternOnFile
	}
	if !isOldEnough(info, now, src.MinAge.Duration) {
		return "", 0, errFileTooYoung
	}
	return selectedFile(src.Path, info, now, src.MinAge.Duration)
}

func resolveDirectory(src config.SourceConfig, now time.Time) (path string, size int64, err error) {
	if src.Pattern != "" {
		if _, err := filepath.Match(src.Pattern, ""); err != nil {
			return "", 0, errInvalidPattern
		}
	}
	entries, err := os.ReadDir(src.Path)
	if err != nil {
		return "", 0, errDirectoryRead
	}

	var selected os.FileInfo
	var selectedName string
	for _, entry := range entries {
		if src.Pattern != "" {
			matches, _ := filepath.Match(src.Pattern, entry.Name())
			if !matches {
				continue
			}
		}
		info, err := entry.Info()
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", 0, errCandidateStat
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if !isOldEnough(info, now, src.MinAge.Duration) {
			continue
		}
		if isBetterCandidate(info, entry.Name(), selected, selectedName) {
			selected = info
			selectedName = entry.Name()
		}
	}
	if selected == nil {
		return "", 0, errNoCandidates
	}
	return selectedFile(filepath.Join(src.Path, selectedName), selected, now, src.MinAge.Duration)
}

func isOldEnough(info os.FileInfo, now time.Time, minAge time.Duration) bool {
	return !info.ModTime().After(now.Add(-minAge))
}

func isBetterCandidate(info os.FileInfo, name string, selected os.FileInfo, selectedName string) bool {
	return selected == nil ||
		info.ModTime().After(selected.ModTime()) ||
		(info.ModTime().Equal(selected.ModTime()) && name < selectedName)
}

func selectedFile(path string, selectedInfo os.FileInfo, now time.Time, minAge time.Duration) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, errSelectedRead
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return "", 0, errSelectedStat
	}
	if err := file.Close(); err != nil {
		return "", 0, errSelectedRead
	}
	if !openedInfo.Mode().IsRegular() {
		return "", 0, errSelectedNotRegular
	}
	if !isOldEnough(openedInfo, now, minAge) {
		return "", 0, errSelectedTooYoung
	}
	if !os.SameFile(selectedInfo, openedInfo) ||
		selectedInfo.Size() != openedInfo.Size() ||
		!selectedInfo.ModTime().Equal(openedInfo.ModTime()) {
		return "", 0, errSelectedChanged
	}
	// The source is not copied; it can still change after this opened-handle check.
	return path, openedInfo.Size(), nil
}
