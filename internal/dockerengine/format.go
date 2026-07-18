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
)

// Engine identifies which restore path a dump needs.
type Engine int

const (
	// EnginePostgresPlain is the fallback for unknown input; psql reports the
	// actual syntax error during restore.
	EnginePostgresPlain Engine = iota
	EnginePostgresCustom
	EngineMySQL
)

// detectEngine recognizes pg_dump custom files and mysqldump headers. Other
// input is treated as plain PostgreSQL.
func detectEngine(dumpPath string) (Engine, error) {
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
