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
	// EnginePostgresPlain is also the fallback for any dump that doesn't
	// match a known signature — psql -f already behaves reasonably (and
	// fails loudly via ON_ERROR_STOP) on unrecognized input.
	EnginePostgresPlain Engine = iota
	EnginePostgresCustom
	EngineMySQL
)

// detectEngine determines which restore path a dump needs by sniffing its
// content:
//   - custom-format pg_dump (-Fc) starts with the "PGDMP" magic bytes
//   - mysqldump plain-SQL output always starts with a "-- MySQL dump" header
//     comment
//   - anything else (including empty/short files) falls back to Postgres
//     plain SQL — psql will fail on it with a clear error instead of us
//     panicking while reading the signature
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
