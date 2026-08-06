package dockerengine

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

const (
	pgCustomSignature  = "PGDMP"
	mysqlDumpSignature = "-- MySQL dump"
	// mongoMetadataSuffix marks a mongodump collection directory: mongodump
	// writes one <collection>.metadata.json per collection alongside its
	// <collection>.bson, plus a prelude.json we don't need to inspect.
	mongoMetadataSuffix = ".metadata.json"
)

// Engine identifies which restore path a dump needs.
type Engine int

const (
	// EnginePostgresPlain is the fallback for unknown input; psql reports the
	// actual syntax error during restore.
	EnginePostgresPlain Engine = iota
	EnginePostgresCustom
	EngineMySQL
	EngineMongo
)

// detectEngine recognizes pg_dump custom files, mysqldump headers, and
// mongodump collection directories. Other input is treated as plain
// PostgreSQL.
func detectEngine(dumpPath string) (Engine, error) {
	info, err := os.Stat(dumpPath)
	if err != nil {
		return EnginePostgresPlain, fmt.Errorf("statting dump: %w", err)
	}
	if info.IsDir() {
		return detectDirectoryEngine(dumpPath)
	}
	return detectFileEngine(dumpPath)
}

// detectDirectoryEngine recognizes a mongodump collection directory (the
// only directory-shaped dump format this agent restores). Local source
// resolution only ever hands a directory to Restore when it already matched
// this same signature, so anything else here is an error rather than a
// silent fallback.
func detectDirectoryEngine(dumpPath string) (Engine, error) {
	if IsMongoDumpDir(dumpPath) {
		return EngineMongo, nil
	}
	return EnginePostgresPlain, fmt.Errorf("unrecognized dump directory %q: no %s file found", dumpPath, mongoMetadataSuffix)
}

func detectFileEngine(dumpPath string) (Engine, error) {
	f, err := os.Open(dumpPath)
	if err != nil {
		return EnginePostgresPlain, fmt.Errorf("opening dump: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Warn("failed to close dump file after engine detection", "path", dumpPath, "error", cerr)
		}
	}()

	sig := make([]byte, len(pgCustomSignature))
	n, err := io.ReadFull(f, sig)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return EnginePostgresPlain, fmt.Errorf("reading dump signature: %w", err)
	}
	if n == len(pgCustomSignature) && string(sig) == pgCustomSignature {
		return EnginePostgresCustom, nil
	}

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return EnginePostgresPlain, fmt.Errorf("seeking dump: %w", err)
	}
	scanner := bufio.NewScanner(f)
	if scanner.Scan() && strings.HasPrefix(scanner.Text(), mysqlDumpSignature) {
		return EngineMySQL, nil
	}

	return EnginePostgresPlain, nil
}

// IsMongoDumpFileSet reports whether names contains a mongodump collection
// signature: at least one *.metadata.json alongside its matching *.bson.
// This is the core signature check, reused by both local directory scanning
// (IsMongoDumpDir) and S3 object key detection (internal/sources/s3).
func IsMongoDumpFileSet(names []string) bool {
	files := make(map[string]struct{}, len(names))
	for _, name := range names {
		files[name] = struct{}{}
	}
	for name := range files {
		if strings.HasSuffix(name, mongoMetadataSuffix) {
			base := strings.TrimSuffix(name, mongoMetadataSuffix)
			if _, ok := files[base+".bson"]; ok {
				return true
			}
		}
	}
	return false
}

// IsMongoDumpDir reports whether path is a directory whose direct children
// include a mongodump collection metadata file (a *.metadata.json alongside
// the matching *.bson). Exported so internal/sources/local can recognize the
// same signature before acquisition, without duplicating it.
func IsMongoDumpDir(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.Type().IsRegular() {
			names = append(names, entry.Name())
		}
	}
	return IsMongoDumpFileSet(names)
}
