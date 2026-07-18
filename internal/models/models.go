// Package models defines the reports sent by the agent to UnDump Cloud.
// Reports contain check results and metrics, never dump contents or credentials.
package models

import "time"

// Status is the outcome of a run.
type Status string

const (
	StatusPass  Status = "pass"
	StatusFail  Status = "fail"
	StatusError Status = "error"
)

// CheckStatus is the outcome of a single check.
type CheckStatus string

const (
	CheckStatusPass CheckStatus = "pass"
	CheckStatusFail CheckStatus = "fail"
)

// CheckResult describes one check against the restored database.
type CheckResult struct {
	Name     string      `json:"name"`
	Status   CheckStatus `json:"status"`
	Detail   string      `json:"detail"`
	Value    *string     `json:"value,omitempty"`
	Expected *string     `json:"expected,omitempty"`
}

// RunReport describes one run for one target.
//
// Error means the restore could not run, fail means a check failed, and pass
// means the restore and all checks succeeded.
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
