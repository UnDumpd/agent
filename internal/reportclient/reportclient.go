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

// Send POSTs the report to {endpoint}/v1/runs. Returns true on successful
// delivery (2xx), false on any network, HTTP, or serialization error.
func Send(ctx context.Context, endpoint, apiKey string, report models.RunReport) bool {
	if endpoint == "" || apiKey == "" {
		slog.Info("cloud not configured (cloud.endpoint/api_key empty), skipping report", "target", report.TargetName)
		return false
	}

	body, err := json.Marshal(report)
	if err != nil {
		slog.Warn("failed to serialize report", "target", report.TargetName, "error", err)
		return false
	}

	url := strings.TrimRight(endpoint, "/") + "/v1/runs"
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		slog.Warn("failed to build report request", "target", report.TargetName, "error", err)
		return false
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Warn("report not delivered", "target", report.TargetName, "error", err)
		return false
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			slog.Warn("failed to close cloud response body", "target", report.TargetName, "error", cerr)
		}
	}()

	if resp.StatusCode >= 300 {
		slog.Warn("cloud returned an error", "target", report.TargetName, "status", resp.StatusCode)
		return false
	}
	return true
}
