package models_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"undump/internal/models"
)

func TestRunReport_JSONFieldNames(t *testing.T) {
	detail := "restore completed without errors"
	rto := 12.5
	size := int64(2801)

	report := models.RunReport{
		TargetName:    "prod-billing",
		Engine:        "postgres",
		SourceURI:     "s3://backups/billing/latest.dump",
		AgentVersion:  "0.1.0",
		StartedAt:     time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
		FinishedAt:    time.Date(2026, 7, 1, 12, 0, 30, 0, time.UTC),
		Status:        models.StatusPass,
		RTOSeconds:    &rto,
		DumpSizeBytes: &size,
		Checks: []models.CheckResult{
			{Name: "restore", Status: models.CheckStatusPass, Detail: detail},
		},
	}

	raw, err := json.Marshal(report)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(raw, &decoded))

	// Keep this list in sync with cloud/app/schemas.py::RunReportIn.
	for _, field := range []string{
		"target_name", "engine", "source_uri", "agent_version",
		"started_at", "finished_at", "status", "rto_seconds",
		"dump_size_bytes", "checks",
	} {
		assert.Contains(t, decoded, field, "field %s is missing from JSON", field)
	}

	checks, ok := decoded["checks"].([]any)
	require.True(t, ok)
	require.Len(t, checks, 1)
	check := checks[0].(map[string]any)
	for _, field := range []string{"name", "status", "detail"} {
		assert.Contains(t, check, field)
	}
	assert.Equal(t, "pass", decoded["status"])
}

func TestRunReport_ChecksNeverNull(t *testing.T) {
	report := models.RunReport{Checks: []models.CheckResult{}}
	raw, err := json.Marshal(report)
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"checks":[]`)
	assert.NotContains(t, string(raw), `"checks":null`)
}
