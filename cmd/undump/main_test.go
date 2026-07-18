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

	report := runTarget(context.Background(), target, nil)

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

func TestRunTarget_ForwardsLastRowcountIntoRowcountCheck(t *testing.T) {
	target := config.Target{
		Name:   "test-target",
		Engine: "postgres",
		Source: config.S3Source{
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
		Source: config.S3Source{
			URI:         "s3://undump-test/dumps/does-not-exist.dump",
			EndpointURL: "http://minio:9000",
			AccessKey:   "minioadmin",
			SecretKey:   "minioadmin",
		},
	}

	report := runTarget(context.Background(), target, nil)

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

	require.Len(t, received, 2)
	assert.Equal(t, string(models.StatusPass), string(received[0].Status))
	assert.Equal(t, string(models.StatusError), string(received[1].Status))
}

func TestStatusFromChecksFailsWhenAnyCheckFails(t *testing.T) {
	status := statusFromChecks([]models.CheckResult{
		{Name: "restore", Status: models.CheckStatusPass},
		{Name: "rowcount", Status: models.CheckStatusFail},
	})

	assert.Equal(t, models.StatusFail, status)
}
