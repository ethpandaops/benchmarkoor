package indexstore

// TestStat represents a single per-test timing entry for suite stats.
type TestStat struct {
	ID        uint   `gorm:"primaryKey"`
	SuiteHash string `gorm:"not null;uniqueIndex:idx_td_suite_test_run;index:idx_ts_suite_run_start;index:idx_ts_suite_start_ttime"`
	RunID     string `gorm:"not null;uniqueIndex:idx_td_suite_test_run;index:idx_ts_run_id"`
	TestName  string `gorm:"not null;uniqueIndex:idx_td_suite_test_run"`
	Client    string
	RunStart  int64 `gorm:"index:idx_ts_suite_run_start;index:idx_ts_suite_start_ttime"`
	RunEnd    int64

	// Total (sum of the setup and test steps). TotalTimeNs is the
	// gas-reporting time plus the time to reject of both steps; TotalMGasS
	// uses only the gas-reporting time.
	TotalGasUsed uint64
	TotalTimeNs  int64
	TotalMGasS   float64 `gorm:"column:total_mgas_s"`

	// SetupRejectedTimeNs and TestRejectedTimeNs are the time the client took
	// to reject the step's payloads that the fixture marks invalid (0 when it
	// has none). A rejected payload reports no gas, so this time is not in
	// SetupTimeNs/TestTimeNs, which stay the denominator of the step MGas/s.
	SetupRejectedTimeNs int64 `gorm:"column:setup_rejected_time_ns;not null;default:0"`
	TestRejectedTimeNs  int64 `gorm:"column:test_rejected_time_ns;not null;default:0"`

	// Setup step.
	SetupGasUsed              uint64
	SetupTimeNs               int64
	SetupMGasS                float64 `gorm:"column:setup_mgas_s"`
	SetupRPCCallsCount        int     `gorm:"column:setup_rpc_calls_count"`
	SetupResourceCPUUsec      uint64  `gorm:"column:setup_resource_cpu_usec"`
	SetupResourceMemDelta     int64   `gorm:"column:setup_resource_memory_delta_bytes"`
	SetupResourceMemBytes     uint64  `gorm:"column:setup_resource_memory_bytes"`
	SetupResourceDiskReadB    uint64  `gorm:"column:setup_resource_disk_read_bytes"`
	SetupResourceDiskWriteB   uint64  `gorm:"column:setup_resource_disk_write_bytes"`
	SetupResourceDiskReadOps  uint64  `gorm:"column:setup_resource_disk_read_iops"`
	SetupResourceDiskWriteOps uint64  `gorm:"column:setup_resource_disk_write_iops"`

	// Test step.
	TestGasUsed              uint64
	TestTimeNs               int64   `gorm:"index:idx_ts_suite_start_ttime"`
	TestMGasS                float64 `gorm:"column:test_mgas_s"`
	TestRPCCallsCount        int     `gorm:"column:test_rpc_calls_count"`
	TestResourceCPUUsec      uint64  `gorm:"column:test_resource_cpu_usec"`
	TestResourceMemDelta     int64   `gorm:"column:test_resource_memory_delta_bytes"`
	TestResourceMemBytes     uint64  `gorm:"column:test_resource_memory_bytes"`
	TestResourceDiskReadB    uint64  `gorm:"column:test_resource_disk_read_bytes"`
	TestResourceDiskWriteB   uint64  `gorm:"column:test_resource_disk_write_bytes"`
	TestResourceDiskReadOps  uint64  `gorm:"column:test_resource_disk_read_iops"`
	TestResourceDiskWriteOps uint64  `gorm:"column:test_resource_disk_write_iops"`
}

// ComputeMGasS calculates megagas per second from gas used and time in
// nanoseconds. Returns 0 if timeNs is non-positive.
func ComputeMGasS(gasUsed uint64, timeNs int64) float64 {
	if timeNs <= 0 {
		return 0
	}

	return float64(gasUsed) / 1_000_000.0 / (float64(timeNs) / 1e9)
}
