package indexstore

// TestStatsBlockLog represents a single per-test block log entry with
// flattened metrics from result.block-logs.json.
type TestStatsBlockLog struct {
	ID        uint   `gorm:"primaryKey"`
	SuiteHash string `gorm:"not null;index:idx_tbl_suite"`
	RunID     string `gorm:"not null;index:idx_tbl_run"`
	TestName  string `gorm:"not null"`
	Client    string

	// Block info.
	BlockNumber  uint64
	BlockHash    string
	BlockGasUsed uint64
	BlockTxCount int

	// Timing (milliseconds).
	TimingExecutionMs float64 `gorm:"column:timing_execution_ms"`
	TimingStateReadMs float64 `gorm:"column:timing_state_read_ms"`
	TimingStateHashMs float64 `gorm:"column:timing_state_hash_ms"`
	TimingCommitMs    float64 `gorm:"column:timing_commit_ms"`
	TimingTotalMs     float64 `gorm:"column:timing_total_ms"`

	// Throughput.
	ThroughputMgasPerSec float64 `gorm:"column:throughput_mgas_per_sec"`

	// State reads.
	StateReadAccounts     int `gorm:"column:state_read_accounts"`
	StateReadStorageSlots int `gorm:"column:state_read_storage_slots"`
	StateReadCode         int `gorm:"column:state_read_code"`
	StateReadCodeBytes    int `gorm:"column:state_read_code_bytes"`

	// State writes.
	StateWriteAccounts        int `gorm:"column:state_write_accounts"`
	StateWriteAccountsDeleted int `gorm:"column:state_write_accounts_deleted"`
	StateWriteStorageSlots    int `gorm:"column:state_write_storage_slots"`
	StateWriteSlotsDeleted    int `gorm:"column:state_write_slots_deleted"`
	StateWriteCode            int `gorm:"column:state_write_code"`
	StateWriteCodeBytes       int `gorm:"column:state_write_code_bytes"`

	// Cache: account.
	CacheAccountHits    int     `gorm:"column:cache_account_hits"`
	CacheAccountMisses  int     `gorm:"column:cache_account_misses"`
	CacheAccountHitRate float64 `gorm:"column:cache_account_hit_rate"`

	// Cache: storage.
	CacheStorageHits    int     `gorm:"column:cache_storage_hits"`
	CacheStorageMisses  int     `gorm:"column:cache_storage_misses"`
	CacheStorageHitRate float64 `gorm:"column:cache_storage_hit_rate"`

	// Cache: code.
	CacheCodeHits      int     `gorm:"column:cache_code_hits"`
	CacheCodeMisses    int     `gorm:"column:cache_code_misses"`
	CacheCodeHitRate   float64 `gorm:"column:cache_code_hit_rate"`
	CacheCodeHitBytes  int     `gorm:"column:cache_code_hit_bytes"`
	CacheCodeMissBytes int     `gorm:"column:cache_code_miss_bytes"`
}
