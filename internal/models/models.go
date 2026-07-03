// Package models describes the agent → cloud contract (RunReport).
//
// The only struct that crosses the boundary of the client's infrastructure.
// Data rows, dumps, or client credentials must NEVER end up here —
// only check results and metrics. A mirrored copy lives in
// cloud/app/schemas.py; when changing fields, update both sides in the same commit.
package models

import "time"

// Status — the run's overall status.
type Status string

const (
	StatusPass  Status = "pass"
	StatusFail  Status = "fail"
	StatusError Status = "error"
)

// CheckStatus — the status of a single check.
type CheckStatus string

const (
	CheckStatusPass CheckStatus = "pass"
	CheckStatusFail CheckStatus = "fail"
)

// CheckResult — the result of a single check against the restored DB.
type CheckResult struct {
	Name     string      `json:"name"`
	Status   CheckStatus `json:"status"`
	Detail   string      `json:"detail"`
	Value    *string     `json:"value,omitempty"`
	Expected *string     `json:"expected,omitempty"`
}

// RunReport — a single check run for a single target.
//
// status:
//
//	error — couldn't even restore/start (no S3, docker unavailable)
//	fail  — restored, but at least one check failed
//	pass  — restored and all checks passed
type RunReport struct {
	TargetName    string        `json:"target_name"`
	Engine        string        `json:"engine"`
	SourceURI     string        `json:"source_uri"`
	AgentVersion  string        `json:"agent_version"`
	StartedAt     time.Time     `json:"started_at"`
	FinishedAt    time.Time     `json:"finished_at"`
	Status        Status        `json:"status"`
	RTOSeconds    *float64      `json:"rto_seconds,omitempty"`
	DumpSizeBytes *int64        `json:"dump_size_bytes,omitempty"`
	Checks        []CheckResult `json:"checks"`
	Error         *string       `json:"error,omitempty"`
}
