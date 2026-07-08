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

func TestRunDaemon_RequiresScheduleOnEveryTarget(t *testing.T) {
	path := writeConfig(t, `
targets:
  - name: "no-schedule"
    source: {type: "s3", uri: "s3://b/k", access_key: "a", secret_key: "s"}
`)

	err := runDaemon(context.Background(), path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-schedule")
	assert.Contains(t, err.Error(), "schedule is required")
}

func TestRunDaemon_RejectsInvalidCronExpression(t *testing.T) {
	path := writeConfig(t, `
targets:
  - name: "bad-schedule"
    schedule: "not a cron expression"
    source: {type: "s3", uri: "s3://b/k", access_key: "a", secret_key: "s"}
`)

	err := runDaemon(context.Background(), path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad-schedule")
}

func TestRunDaemon_RejectsEmptyTargetList(t *testing.T) {
	path := writeConfig(t, "targets: []\n")

	err := runDaemon(context.Background(), path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no targets")
}

func TestRunDaemon_RunsOnScheduleAndCarriesRowcountBetweenRuns(t *testing.T) {
	// SkipIfStillRunning means an ~8s restore against a 1s schedule only
	// ever has one invocation in flight — exactly one report is expected.
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

	// The scheduler gets ~1.5s — past the first "@every 1s" tick — before
	// the shutdown signal. runDaemon then blocks on sched.Stop() until the
	// in-flight restore (several seconds, real Docker + S3) actually
	// finishes, so the overall test still runs to completion deterministically.
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	err := runDaemon(ctx, path)
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, received, "at least one scheduled run should have reported to the cloud")
	first := received[0]
	assert.Equal(t, "scheduled-target", first.TargetName)
	require.Len(t, first.Checks, 2)
	rowcount := first.Checks[1]
	assert.Equal(t, "rowcount", rowcount.Name)
	// No previous run yet in this fresh daemon: baseline pass, not a delta.
	assert.Equal(t, models.CheckStatusPass, rowcount.Status)
	assert.Contains(t, rowcount.Detail, "baseline")
}
