package indexstore

// TestDuration represents a single per-test timing entry for suite stats.
type TestDuration struct {
	ID        uint   `gorm:"primaryKey"`
	SuiteHash string `gorm:"not null;uniqueIndex:idx_td_suite_test_run"`
	TestName  string `gorm:"not null;uniqueIndex:idx_td_suite_test_run"`
	RunID     string `gorm:"not null;uniqueIndex:idx_td_suite_test_run"`
	Client    string
	RunStart  int64
	RunEnd    int64

	// Total (sum of all steps).
	TotalGasUsed uint64
	TotalTimeNs  int64
	TotalMGasS   float64 `gorm:"column:total_mgas_s"`

	// Setup step.
	SetupGasUsed uint64
	SetupTimeNs  int64
	SetupMGasS   float64 `gorm:"column:setup_mgas_s"`

	// Test step.
	TestGasUsed uint64
	TestTimeNs  int64
	TestMGasS   float64 `gorm:"column:test_mgas_s"`

	// Per-step stats serialized as JSON (kept for raw access).
	StepsJSON string `gorm:"type:text"`
}

// ComputeMGasS calculates megagas per second from gas used and time in
// nanoseconds. Returns 0 if timeNs is non-positive.
func ComputeMGasS(gasUsed uint64, timeNs int64) float64 {
	if timeNs <= 0 {
		return 0
	}

	return float64(gasUsed) / 1_000_000.0 / (float64(timeNs) / 1e9)
}
