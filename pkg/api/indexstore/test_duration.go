package indexstore

// TestDuration represents a single per-test timing entry for suite stats.
type TestDuration struct {
	ID        uint   `gorm:"primaryKey"`
	SuiteHash string `gorm:"not null;uniqueIndex:idx_td_suite_test_run"`
	TestName  string `gorm:"not null;uniqueIndex:idx_td_suite_test_run"`
	RunID     string `gorm:"not null;uniqueIndex:idx_td_suite_test_run"`
	Client    string
	GasUsed   uint64
	TimeNs    int64
	RunStart  int64
	RunEnd    int64

	// Per-step stats serialized as JSON.
	StepsJSON string `gorm:"type:text"`
}
