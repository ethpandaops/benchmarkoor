package indexstore

import "time"

// Run represents a single indexed benchmark run in the database.
type Run struct {
	ID                uint   `gorm:"primaryKey"`
	DiscoveryPath     string `gorm:"not null;uniqueIndex:idx_runs_dp_run"`
	RunID             string `gorm:"not null;uniqueIndex:idx_runs_dp_run"`
	Timestamp         int64
	TimestampEnd      int64
	SuiteHash         string `gorm:"index"`
	Status            string
	TerminationReason string
	HasResult         bool

	// Denormalized instance fields.
	InstanceID       string
	Client           string `gorm:"index"`
	Image            string
	RollbackStrategy string

	// Denormalized test stats.
	TestsTotal  int
	TestsPassed int
	TestsFailed int

	// Per-step stats serialized as JSON.
	StepsJSON string `gorm:"type:text"`

	IndexedAt   time.Time
	ReindexedAt *time.Time
}
