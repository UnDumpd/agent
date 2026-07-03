package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"undump/internal/config"
	"undump/internal/models"
)

func TestRunTarget_SuccessfulRestoreEndToEnd(t *testing.T) {
	target := config.Target{
		Name:   "test-target",
		Engine: "postgres",
		Source: config.S3Source{
			URI:         "s3://undump-test/dumps/exact.dump",
			EndpointURL: "http://minio:9000",
			AccessKey:   "minioadmin",
			SecretKey:   "minioadmin",
		},
	}

	report := runTarget(context.Background(), target)

	assert.Equal(t, models.StatusPass, report.Status)
	require.Len(t, report.Checks, 1)
	assert.Equal(t, "restore", report.Checks[0].Name)
	assert.Equal(t, models.CheckStatusPass, report.Checks[0].Status)
	require.NotNil(t, report.RTOSeconds)
	assert.Greater(t, *report.RTOSeconds, 0.0)
	require.NotNil(t, report.DumpSizeBytes)
	assert.Equal(t, int64(2801), *report.DumpSizeBytes)
	assert.Nil(t, report.Error)
}

func TestRunTarget_UnknownS3KeyGivesErrorStatus(t *testing.T) {
	target := config.Target{
		Name: "broken-target",
		Source: config.S3Source{
			URI:         "s3://undump-test/dumps/does-not-exist.dump",
			EndpointURL: "http://minio:9000",
			AccessKey:   "minioadmin",
			SecretKey:   "minioadmin",
		},
	}

	report := runTarget(context.Background(), target)

	assert.Equal(t, models.StatusError, report.Status)
	require.NotNil(t, report.Error)
	assert.Empty(t, report.Checks)
}

func TestRunCheck_SendsReportsToCloudAndContinuesOnTargetError(t *testing.T) {
	var received []models.RunReport
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report models.RunReport
		require.NoError(t, json.NewDecoder(r.Body).Decode(&report))
		received = append(received, report)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	configYAML := `
cloud:
  endpoint: "` + server.URL + `"
  api_key: "test-key"
targets:
  - name: "ok-target"
    engine: "postgres"
    source:
      type: "s3"
      uri: "s3://undump-test/dumps/exact.dump"
      endpoint_url: "http://minio:9000"
      access_key: "minioadmin"
      secret_key: "minioadmin"
  - name: "broken-target"
    engine: "postgres"
    source:
      type: "s3"
      uri: "s3://undump-test/dumps/does-not-exist.dump"
      endpoint_url: "http://minio:9000"
      access_key: "minioadmin"
      secret_key: "minioadmin"
`
	path := filepath.Join(t.TempDir(), "undump.yaml")
	require.NoError(t, os.WriteFile(path, []byte(configYAML), 0644))

	require.NoError(t, runCheck(context.Background(), path))

	require.Len(t, received, 2, "both targets should have sent a report — one failing should not abort the loop")
	assert.Equal(t, string(models.StatusPass), string(received[0].Status))
	assert.Equal(t, string(models.StatusError), string(received[1].Status))
}
