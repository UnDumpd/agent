package local_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"undump/internal/config"
	"undump/internal/sources/local"
)

func TestResolve_ExactEligibleFile(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "backup.dump")
	require.NoError(t, os.WriteFile(path, []byte("dump"), 0o600))
	require.NoError(t, os.Chtimes(path, now.Add(-5*time.Minute), now.Add(-5*time.Minute)))

	gotPath, gotSize, err := local.Resolve(config.SourceConfig{
		Path:   path,
		MinAge: config.Duration{Duration: 5 * time.Minute, Set: true},
	}, now)

	require.NoError(t, err)
	assert.Equal(t, path, gotPath)
	assert.Equal(t, int64(4), gotSize)
}

func TestResolve_ExactFileRejectsPattern(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "backup.dump")
	require.NoError(t, os.WriteFile(path, []byte("dump"), 0o600))
	require.NoError(t, os.Chtimes(path, now.Add(-10*time.Minute), now.Add(-10*time.Minute)))

	_, _, err := local.Resolve(config.SourceConfig{
		Path:    path,
		Pattern: "*.dump",
		MinAge:  config.Duration{Duration: 5 * time.Minute, Set: true},
	}, now)

	require.EqualError(t, err, "local source: pattern requires a directory")
}

func TestResolve_DirectorySelectsNewestMatchingEligibleFile(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	writeFileAt(t, filepath.Join(dir, "older.dump"), "old", now.Add(-20*time.Minute))
	wantPath := filepath.Join(dir, "newer.dump")
	writeFileAt(t, wantPath, "newest", now.Add(-10*time.Minute))
	writeFileAt(t, filepath.Join(dir, "ignored.txt"), "ignore", now.Add(-6*time.Minute))

	gotPath, gotSize, err := local.Resolve(config.SourceConfig{
		Path:    dir,
		Pattern: "*.dump",
		MinAge:  config.Duration{Duration: 5 * time.Minute, Set: true},
	}, now)

	require.NoError(t, err)
	assert.Equal(t, wantPath, gotPath)
	assert.Equal(t, int64(6), gotSize)
}

func TestResolve_DirectoryRejectsInvalidPatternEvenWhenEmpty(t *testing.T) {
	dir := t.TempDir()

	_, _, err := local.Resolve(config.SourceConfig{
		Path:    dir,
		Pattern: "[",
	}, time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC))

	require.EqualError(t, err, "local source: pattern is invalid")
}

func TestResolve_DirectorySelectionRules(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		pattern string
		setup   func(t *testing.T, dir string) string
	}{
		{
			name: "empty pattern accepts every regular file",
			setup: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, "backup.custom")
				writeFileAt(t, path, "dump", now.Add(-10*time.Minute))
				return path
			},
		},
		{
			name: "files younger than min age are skipped",
			setup: func(t *testing.T, dir string) string {
				want := filepath.Join(dir, "eligible.dump")
				writeFileAt(t, want, "eligible", now.Add(-10*time.Minute))
				writeFileAt(t, filepath.Join(dir, "too-young.dump"), "young", now.Add(-time.Minute))
				return want
			},
		},
		{
			name:    "scan does not recurse",
			pattern: "*.dump",
			setup: func(t *testing.T, dir string) string {
				want := filepath.Join(dir, "direct.dump")
				writeFileAt(t, want, "direct", now.Add(-10*time.Minute))
				nested := filepath.Join(dir, "nested")
				require.NoError(t, os.Mkdir(nested, 0o700))
				writeFileAt(t, filepath.Join(nested, "newer.dump"), "nested", now.Add(-6*time.Minute))
				return want
			},
		},
		{
			name:    "equal mtimes choose lexicographically smallest basename",
			pattern: "*.dump",
			setup: func(t *testing.T, dir string) string {
				modTime := now.Add(-10 * time.Minute)
				writeFileAt(t, filepath.Join(dir, "z.dump"), "z", modTime)
				want := filepath.Join(dir, "a.dump")
				writeFileAt(t, want, "a", modTime)
				return want
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			wantPath := tt.setup(t, dir)

			gotPath, _, err := local.Resolve(config.SourceConfig{
				Path:    dir,
				Pattern: tt.pattern,
				MinAge:  config.Duration{Duration: 5 * time.Minute, Set: true},
			}, now)

			require.NoError(t, err)
			assert.Equal(t, wantPath, gotPath)
		})
	}
}

func TestResolve_AcquisitionErrorsAreUsefulAndPathPrivate(t *testing.T) {
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		setup   func(t *testing.T) config.SourceConfig
		wantErr string
	}{
		{
			name: "missing configured path",
			setup: func(t *testing.T) config.SourceConfig {
				return config.SourceConfig{Path: filepath.Join(t.TempDir(), "missing.dump")}
			},
			wantErr: "local source: configured path is unavailable",
		},
		{
			name: "exact file is too young",
			setup: func(t *testing.T) config.SourceConfig {
				path := filepath.Join(t.TempDir(), "young.dump")
				writeFileAt(t, path, "dump", now.Add(-time.Minute))
				return config.SourceConfig{
					Path:   path,
					MinAge: config.Duration{Duration: 5 * time.Minute, Set: true},
				}
			},
			wantErr: "local source: configured file is younger than min_age",
		},
		{
			name: "exact file has a pattern",
			setup: func(t *testing.T) config.SourceConfig {
				path := filepath.Join(t.TempDir(), "backup.dump")
				writeFileAt(t, path, "dump", now.Add(-10*time.Minute))
				return config.SourceConfig{Path: path, Pattern: "*.dump"}
			},
			wantErr: "local source: pattern requires a directory",
		},
		{
			name: "directory has no eligible candidates",
			setup: func(t *testing.T) config.SourceConfig {
				dir := t.TempDir()
				writeFileAt(t, filepath.Join(dir, "young.dump"), "dump", now.Add(-time.Minute))
				return config.SourceConfig{
					Path:   dir,
					MinAge: config.Duration{Duration: 5 * time.Minute, Set: true},
				}
			},
			wantErr: "local source: no eligible backup files",
		},
		{
			name: "nonregular direct children are not candidates",
			setup: func(t *testing.T) config.SourceConfig {
				dir := t.TempDir()
				require.NoError(t, os.Mkdir(filepath.Join(dir, "looks-like.dump"), 0o700))
				return config.SourceConfig{Path: dir}
			},
			wantErr: "local source: no eligible backup files",
		},
		{
			name: "directory pattern is invalid",
			setup: func(t *testing.T) config.SourceConfig {
				return config.SourceConfig{Path: t.TempDir(), Pattern: "["}
			},
			wantErr: "local source: pattern is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := tt.setup(t)

			_, _, err := local.Resolve(src, now)

			require.EqualError(t, err, tt.wantErr)
			assertPathPrivateError(t, err, src.Path)
		})
	}
}

func TestResolve_RejectsNonRegularExactPath(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.dump")
	writeFileAt(t, target, "dump", time.Date(2026, 7, 27, 11, 0, 0, 0, time.UTC))
	link := filepath.Join(dir, "configured.dump")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}

	_, _, err := local.Resolve(
		config.SourceConfig{Path: link},
		time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC),
	)

	require.EqualError(t, err, "local source: configured path is not a regular file or directory")
	assertPathPrivateError(t, err, link)
}

func TestResolve_DirectoryReadErrorIsPathPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permission semantics differ on Windows")
	}
	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	_, _, err := local.Resolve(
		config.SourceConfig{Path: dir},
		time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC),
	)
	if err != nil && err.Error() == "local source: no eligible backup files" {
		t.Skip("current user can read a mode-000 directory")
	}

	require.EqualError(t, err, "local source: configured directory cannot be read")
	assertPathPrivateError(t, err, dir)
}

func TestResolve_SelectedFileReadErrorIsPathPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission semantics differ on Windows")
	}
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "unreadable.dump")
	writeFileAt(t, path, "dump", now.Add(-time.Hour))
	require.NoError(t, os.Chmod(path, 0))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })

	_, _, err := local.Resolve(config.SourceConfig{Path: path}, now)
	if err == nil {
		t.Skip("current user can read a mode-000 file")
	}

	require.EqualError(t, err, "local source: selected backup file cannot be read")
	assertPathPrivateError(t, err, path)
}

func TestResolve_DirectoryIgnoresEntriesDisappearingDuringScan(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		volatileName string
	}{
		{name: "nonmatching entry", volatileName: "volatile.tmp"},
		{name: "matching entry", volatileName: "volatile.dump"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			want := filepath.Join(dir, "stable.dump")
			writeFileAt(t, want, "stable", now.Add(-time.Hour))
			stopChurn := churnFile(t, filepath.Join(dir, tt.volatileName))
			defer stopChurn()

			for range 500 {
				gotPath, _, err := local.Resolve(config.SourceConfig{
					Path:    dir,
					Pattern: "*.dump",
				}, now)

				require.NoError(t, err)
				assert.Equal(t, want, gotPath)
			}
		})
	}
}

func TestResolve_RejectsFileReplacedDuringAcquisition(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("atomic replacement of an existing file has different Windows semantics")
	}
	previousProcs := runtime.GOMAXPROCS(2)
	defer runtime.GOMAXPROCS(previousProcs)

	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	path := filepath.Join(dir, "backup.dump")
	spare := filepath.Join(dir, "replacement.dump")
	require.NoError(t, os.WriteFile(path, []byte("a"), 0o600))
	require.NoError(t, os.WriteFile(spare, []byte(strings.Repeat("b", 4096)), 0o600))

	stop := make(chan struct{})
	done := make(chan struct{})
	churnErr := make(chan error, 1)
	go func() {
		defer close(done)
		size := 1
		for {
			select {
			case <-stop:
				return
			default:
			}
			if err := os.Rename(spare, path); err != nil {
				churnErr <- err
				return
			}
			contents := "a"
			if size == 4096 {
				contents = strings.Repeat("b", size)
			}
			if err := os.WriteFile(spare, []byte(contents), 0o600); err != nil {
				churnErr <- err
				return
			}
			if size == 1 {
				size = 4096
			} else {
				size = 1
			}
		}
	}()
	defer func() {
		close(stop)
		<-done
	}()

	sawReplacement := false
	for range 20000 {
		_, size, err := local.Resolve(config.SourceConfig{Path: path}, now)
		if err != nil {
			if err.Error() == "local source: selected backup file changed during acquisition" {
				assertPathPrivateError(t, err, path)
				sawReplacement = true
				break
			}
			require.NoError(t, err)
		}
		assert.Contains(t, []int64{1, 4096}, size)
		runtime.Gosched()
		select {
		case err := <-churnErr:
			require.NoError(t, err)
		default:
		}
	}

	assert.True(t, sawReplacement, "expected to observe an atomic replacement race")
}

func churnFile(t *testing.T, path string) func() {
	t.Helper()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			_ = os.WriteFile(path, []byte("volatile"), 0o600)
			_ = os.Remove(path)
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}

func assertPathPrivateError(t *testing.T, err error, configuredPath string) {
	t.Helper()
	require.Error(t, err)
	assert.NotContains(t, err.Error(), configuredPath)
	assert.NotContains(t, err.Error(), filepath.Dir(configuredPath))
}

func writeFileAt(t *testing.T, path, contents string, modTime time.Time) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	require.NoError(t, os.Chtimes(path, modTime, modTime))
}
