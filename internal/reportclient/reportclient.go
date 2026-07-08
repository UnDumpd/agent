// Package reportclient sends a RunReport to the UnDump cloud.
//
// A cloud network error must NOT fail the check run: the check itself
// matters more than delivering the report — that's why Send returns a bool
// rather than an error the caller would have to handle.
package reportclient

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"undump/internal/models"
)

const timeout = 15 * time.Second

// IngestResponse mirrors cloud/app/schemas.py:IngestResponse — the cloud's
// reply to a delivered report. LastRowcount is the target's last known good
// (passing) rowcount, the delta base for that target's next rowcount check.
type IngestResponse struct {
	RunID        int64  `json:"run_id"`
	LastRowcount *int64 `json:"last_rowcount"`
}

// Send POSTs the report to {endpoint}/v1/runs. The bool reports successful
// delivery (2xx) regardless of whether the response body could be parsed —
// callers that don't need IngestResponse can ignore it. On any network, HTTP,
// or serialization error, delivery is false and IngestResponse is zero-valued.
func Send(ctx context.Context, endpoint, apiKey string, report models.RunReport) (IngestResponse, bool) {
	if endpoint == "" || apiKey == "" {
		slog.Info("cloud not configured (cloud.endpoint/api_key empty), skipping report", "target", report.TargetName)
		return IngestResponse{}, false
	}

	body, err := json.Marshal(report)
	if err != nil {
		slog.Warn("failed to serialize report", "target", report.TargetName, "error", err)
		return IngestResponse{}, false
	}

	url := strings.TrimRight(endpoint, "/") + "/v1/runs"
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		slog.Warn("failed to build report request", "target", report.TargetName, "error", err)
		return IngestResponse{}, false
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("report not delivered", "target", report.TargetName, "error", err)
		return IngestResponse{}, false
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("failed to close cloud response body", "target", report.TargetName, "error", cerr)
		}
	}()

	if resp.StatusCode >= 300 {
		slog.Warn("cloud returned an error", "target", report.TargetName, "status", resp.StatusCode)
		return IngestResponse{}, false
	}

	var ingestResp IngestResponse
	if err := json.NewDecoder(resp.Body).Decode(&ingestResp); err != nil {
		slog.Warn("delivered but failed to decode cloud response", "target", report.TargetName, "error", err)
		return IngestResponse{}, true
	}
	return ingestResp, true
}
