package indexstore

import (
	"encoding/json"
	"time"
)

// LiveRun represents an in-progress (or recently-finished but not yet
// indexed) run that a runner is reporting status for. Live runs live in
// their own table so they never interfere with the canonical Run table
// that the on-disk indexer maintains. Once the on-disk indexer picks up
// the same (discovery_path, run_id) pair, the live row is deleted.
//
// Stale live rows (those whose runner stopped reporting) are also deleted
// after a configurable threshold to keep this table small.
type LiveRun struct {
	ID                uint   `gorm:"primaryKey"`
	DiscoveryPath     string `gorm:"not null;uniqueIndex:idx_live_runs_dp_run"`
	RunID             string `gorm:"not null;uniqueIndex:idx_live_runs_dp_run"`
	Timestamp         int64  `gorm:"index"`
	TimestampEnd      int64
	SuiteHash         string
	Status            string
	TerminationReason string

	InstanceID       string
	Client           string `gorm:"index"`
	Image            string
	RollbackStrategy string

	TestsTotal  int
	TestsPassed int
	TestsFailed int

	// Run metadata labels serialized as JSON.
	MetadataJSON string `gorm:"type:text"`

	// ConfigJSON is the latest full run config.json snapshot reported by
	// the runner (matches the on-disk config.json schema). Empty until
	// the runner has built its RunConfig.
	ConfigJSON string `gorm:"type:text"`

	// LastReportedAt is updated on every report from the runner.
	LastReportedAt time.Time `gorm:"index"`
}

// LiveRunReport is the payload a runner sends to POST /api/v1/ingest/runs
// to update the live status of a run in progress.
type LiveRunReport struct {
	DiscoveryPath     string            `json:"discovery_path"`
	RunID             string            `json:"run_id"`
	Timestamp         int64             `json:"timestamp"`
	TimestampEnd      int64             `json:"timestamp_end,omitempty"`
	SuiteHash         string            `json:"suite_hash,omitempty"`
	Status            string            `json:"status"`
	TerminationReason string            `json:"termination_reason,omitempty"`
	InstanceID        string            `json:"instance_id,omitempty"`
	Client            string            `json:"client,omitempty"`
	Image             string            `json:"image,omitempty"`
	RollbackStrategy  string            `json:"rollback_strategy,omitempty"`
	TestsTotal        int               `json:"tests_total"`
	TestsPassed       int               `json:"tests_passed"`
	TestsFailed       int               `json:"tests_failed"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	// Config is the full run config.json content at the time of the report.
	// Passed through verbatim to the UI so it can render the normal run
	// configuration panels without needing the file to be on disk yet.
	Config json.RawMessage `json:"config,omitempty"`
}

// LiveRunResponse is the JSON DTO returned by the live runs endpoint.
type LiveRunResponse struct {
	ID                uint              `json:"id"`
	DiscoveryPath     string            `json:"discovery_path"`
	RunID             string            `json:"run_id"`
	Timestamp         int64             `json:"timestamp"`
	TimestampEnd      int64             `json:"timestamp_end,omitempty"`
	SuiteHash         string            `json:"suite_hash,omitempty"`
	Status            string            `json:"status"`
	TerminationReason string            `json:"termination_reason,omitempty"`
	InstanceID        string            `json:"instance_id,omitempty"`
	Client            string            `json:"client,omitempty"`
	Image             string            `json:"image,omitempty"`
	RollbackStrategy  string            `json:"rollback_strategy,omitempty"`
	TestsTotal        int               `json:"tests_total"`
	TestsPassed       int               `json:"tests_passed"`
	TestsFailed       int               `json:"tests_failed"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	Config            json.RawMessage   `json:"config,omitempty"`
	LastReportedAt    string            `json:"last_reported_at"`
}
