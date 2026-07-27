package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"undump/internal/config"
	"undump/internal/models"
)

func TestRunTarget_SuccessfulRestoreEndToEnd(t *testing.T) {
	target := config.Target{
		Name:   "test-target",
		Engine: "postgres",
		Source: config.SourceConfig{
			Type:        "s3",
			URI:         "s3://undump-test/dumps/exact.dump",
			EndpointURL: "http://minio:9000",
			AccessKey:   "minioadmin",
			SecretKey:   "minioadmin",
		},
	}

	report := runTarget(context.Background(), target, nil)

	assert.Equal(t, models.StatusPass, report.Status)
	require.Len(t, report.Checks, 1)
	assert.Equal(t, "restore", report.Checks[0].Name)
	assert.Equal(t, models.CheckStatusPass, report.Checks[0].Status)
	require.NotNil(t, report.RTOSeconds)
	assert.Greater(t, *report.RTOSeconds, 0.0)
	require.NotNil(t, report.DumpSizeBytes)
	assert.Equal(t, int64(2801), *report.DumpSizeBytes)
	assert.Equal(t, target.Source.URI, report.SourceURI)
	assert.Nil(t, report.Error)
}

func TestRunTarget_SuccessfulLocalRestoreReportsArtifact(t *testing.T) {
	path, err := filepath.Abs("../../testdata/sample_plain.sql")
	require.NoError(t, err)
	info, err := os.Stat(path)
	require.NoError(t, err)
	target := config.Target{
		Name:   "local-target",
		Engine: "postgres",
		Source: config.SourceConfig{
			Type: "local",
			Path: path,
		},
	}

	report := runTarget(context.Background(), target, nil)

	assert.Equal(t, models.StatusPass, report.Status)
	assert.Equal(t, "local://sample_plain.sql", report.SourceURI)
	require.NotNil(t, report.DumpSizeBytes)
	assert.Equal(t, info.Size(), *report.DumpSizeBytes)
	assert.Nil(t, report.Error)
	afterRestore, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, info.Size(), afterRestore.Size())
	assert.Equal(t, info.ModTime(), afterRestore.ModTime())
}

func TestPrepareAcquisitionDest_LocalSourceNeedsNoTempDirectory(t *testing.T) {
	destDir, err := prepareAcquisitionDest(config.SourceConfig{Type: "local"})

	require.NoError(t, err)
	assert.Empty(t, destDir)
}

func TestRunTarget_UnavailableOrTooYoungLocalSourceIsPathPrivate(t *testing.T) {
	tests := []struct {
		name   string
		source func(t *testing.T) config.SourceConfig
		reason string
	}{
		{
			name: "unavailable",
			source: func(t *testing.T) config.SourceConfig {
				return config.SourceConfig{
					Type: "local",
					Path: filepath.Join(t.TempDir(), "missing.dump"),
				}
			},
			reason: "configured path is unavailable",
		},
		{
			name: "too young",
			source: func(t *testing.T) config.SourceConfig {
				path := filepath.Join(t.TempDir(), "young.dump")
				require.NoError(t, os.WriteFile(path, []byte("dump"), 0o600))
				return config.SourceConfig{
					Type:   "local",
					Path:   path,
					MinAge: config.Duration{Duration: time.Hour, Set: true},
				}
			},
			reason: "configured file is younger than min_age",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := tt.source(t)

			report := runTarget(context.Background(), config.Target{
				Name:   "local-target",
				Engine: "postgres",
				Source: source,
			}, nil)

			assert.Equal(t, models.StatusError, report.Status)
			assert.Equal(t, "local://unresolved", report.SourceURI)
			require.NotNil(t, report.Error)
			assert.Equal(t, "acquiring dump: local source: "+tt.reason, *report.Error)
			assert.NotContains(t, *report.Error, source.Path)
			assert.NotContains(t, *report.Error, filepath.Dir(source.Path))
			assert.Empty(t, report.Checks)
		})
	}
}

func TestFinalizeRuntimeError_LocalUsesExactGenericMessage(t *testing.T) {
	tests := []struct {
		name         string
		rawError     string
		localMessage string
	}{
		{
			name:         "unix path with unusual filename characters",
			rawError:     `restoring: opening /srv/private/weird:$#[] name.dump: permission denied`,
			localMessage: "restoring local backup: failed",
		},
		{
			name:         "windows path with unusual filename characters",
			rawError:     `restoring: opening C:\private\weird !#$%&'()+,;=@[]^_.dump: access denied`,
			localMessage: "restoring local backup: failed",
		},
		{
			name:         "parent directory only during check",
			rawError:     `running check rowcount: scanning //server/private/backups: I/O error`,
			localMessage: "running local backup check rowcount: failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := finalizeRuntimeError(
				models.RunReport{},
				time.Time{},
				config.SourceConfig{Type: "local"},
				tt.localMessage,
				errors.New(tt.rawError),
			)

			require.NotNil(t, report.Error)
			assert.Equal(t, models.StatusError, report.Status)
			assert.Equal(t, tt.localMessage, *report.Error)
			assert.NotContains(t, *report.Error, tt.rawError)
		})
	}
}

func TestFinalizeRuntimeError_S3PreservesRawDetail(t *testing.T) {
	raw := errors.New(`restoring: opening C:\Temp\undump-123\dump.sql: access denied`)

	report := finalizeRuntimeError(
		models.RunReport{},
		time.Time{},
		config.SourceConfig{Type: "s3"},
		"restoring local backup: failed",
		raw,
	)

	require.NotNil(t, report.Error)
	assert.Equal(t, raw.Error(), *report.Error)
}

func TestRunTarget_ForwardsLastRowcountIntoRowcountCheck(t *testing.T) {
	target := config.Target{
		Name:   "test-target",
		Engine: "postgres",
		Source: config.SourceConfig{
			Type:        "s3",
			URI:         "s3://undump-test/dumps/exact.sql",
			EndpointURL: "http://minio:9000",
			AccessKey:   "minioadmin",
			SecretKey:   "minioadmin",
		},
		Checks: []config.CheckConfig{{Type: "rowcount", Table: "widgets", MaxDropPct: 10}},
	}

	// Three rows against a previous four exceeds the 10% drop threshold.
	previous := int64(4)
	report := runTarget(context.Background(), target, &previous)

	require.Len(t, report.Checks, 2)
	rowcount := report.Checks[1]
	assert.Equal(t, "rowcount", rowcount.Name)
	assert.Equal(t, models.CheckStatusFail, rowcount.Status)
	assert.Equal(t, models.StatusFail, report.Status)
}

func TestRunTarget_UnknownS3KeyGivesErrorStatus(t *testing.T) {
	target := config.Target{
		Name: "broken-target",
		Source: config.SourceConfig{
			Type:        "s3",
			URI:         "s3://undump-test/dumps/does-not-exist.dump",
			EndpointURL: "http://minio:9000",
			AccessKey:   "minioadmin",
			SecretKey:   "minioadmin",
		},
	}

	report := runTarget(context.Background(), target, nil)

	assert.Equal(t, models.StatusError, report.Status)
	require.NotNil(t, report.Error)
	assert.Contains(t, *report.Error, "downloading dump:")
	assert.Equal(t, target.Source.URI, report.SourceURI)
	assert.Empty(t, report.Checks)
}

func TestRunCheck_ReportsLaterTargetsAfterLocalSourceError(t *testing.T) {
	var received []models.RunReport
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report models.RunReport
		require.NoError(t, json.NewDecoder(r.Body).Decode(&report))
		received = append(received, report)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	configDir := t.TempDir()
	missingPath := filepath.Join(configDir, "private", "missing.dump")
	configYAML := `
cloud:
  endpoint: "` + server.URL + `"
  api_key: "test-key"
targets:
  - name: "broken-local-target"
    engine: "postgres"
    source:
      type: "local"
      path: "` + filepath.ToSlash(missingPath) + `"
  - name: "later-ok-target"
    engine: "postgres"
    source:
      type: "s3"
      uri: "s3://undump-test/dumps/exact.dump"
      endpoint_url: "http://minio:9000"
      access_key: "minioadmin"
      secret_key: "minioadmin"
`
	path := filepath.Join(configDir, "undump.yaml")
	require.NoError(t, os.WriteFile(path, []byte(configYAML), 0644))

	require.NoError(t, runCheck(context.Background(), path))

	require.Len(t, received, 2)
	assert.Equal(t, "broken-local-target", received[0].TargetName)
	assert.Equal(t, models.StatusError, received[0].Status)
	assert.Equal(t, "local://unresolved", received[0].SourceURI)
	require.NotNil(t, received[0].Error)
	assert.NotContains(t, *received[0].Error, missingPath)
	assert.NotContains(t, *received[0].Error, filepath.Dir(missingPath))
	assert.Equal(t, "later-ok-target", received[1].TargetName)
	assert.Equal(t, models.StatusPass, received[1].Status)
}

func TestStatusFromChecksFailsWhenAnyCheckFails(t *testing.T) {
	status := statusFromChecks([]models.CheckResult{
		{Name: "restore", Status: models.CheckStatusPass},
		{Name: "rowcount", Status: models.CheckStatusFail},
	})

	assert.Equal(t, models.StatusFail, status)
}
