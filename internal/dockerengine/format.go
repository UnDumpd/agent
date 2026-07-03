package dockerengine

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
)

const customDumpSignature = "PGDMP"

// detectFormat determines whether the dump is in custom format (pg_dump -Fc,
// starts with "PGDMP") or plain SQL. An empty/short file is treated as plain
// SQL — psql will fail on it with a clear error instead of us panicking while
// reading the signature.
func detectFormat(dumpPath string) (isCustom bool, err error) {
	f, err := os.Open(dumpPath)
	if err != nil {
		return false, fmt.Errorf("opening dump: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Warn("failed to close dump file after format detection", "path", dumpPath, "error", cerr)
		}
	}()

	sig := make([]byte, len(customDumpSignature))
	_, err = io.ReadFull(f, sig)
	if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading dump signature: %w", err)
	}
	return string(sig) == customDumpSignature, nil
}
