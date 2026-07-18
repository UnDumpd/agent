package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"undump/internal/models"
)

func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "undump.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0644))
	return path
}

func TestRunDaemon_ValidatesConfig(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"empty target list", "targets: []\n", "no targets"},
		{"missing schedule", `
targets:
  - name: "no-schedule"
    source: {type: "s3", uri: "s3://b/k", access_key: "a", secret_key: "s"}
`, "schedule is required"},
		{"invalid schedule", `
targets:
  - name: "bad-schedule"
    schedule: "not a cron expression"
    source: {type: "s3", uri: "s3://b/k", access_key: "a", secret_key: "s"}
`, "bad-schedule"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := runDaemon(context.Background(), writeConfig(t, tt.yaml))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestRunDaemon_RunsOnScheduleAndWaitsForCompletion(t *testing.T) {
	var mu sync.Mutex
	var received []models.RunReport
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var report models.RunReport
		require.NoError(t, json.NewDecoder(r.Body).Decode(&report))

		mu.Lock()
		received = append(received, report)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"run_id": 1, "last_rowcount": 3}`))
	}))
	defer server.Close()

	path := writeConfig(t, `
cloud:
  endpoint: "`+server.URL+`"
  api_key: "test-key"
targets:
  - name: "scheduled-target"
    engine: "postgres"
    schedule: "@every 1s"
    source:
      type: "s3"
      uri: "s3://undump-test/dumps/exact.sql"
      endpoint_url: "http://minio:9000"
      access_key: "minioadmin"
      secret_key: "minioadmin"
    checks:
      - type: "rowcount"
        table: "widgets"
`)

	// Cancel after the first tick; runDaemon still waits for that restore.
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	err := runDaemon(ctx, path)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.Len(t, received, 1)
	first := received[0]
	assert.Equal(t, "scheduled-target", first.TargetName)
	require.Len(t, first.Checks, 2)
	rowcount := first.Checks[1]
	assert.Equal(t, "rowcount", rowcount.Name)
	assert.Equal(t, models.CheckStatusPass, rowcount.Status)
	assert.Contains(t, rowcount.Detail, "baseline")
}
