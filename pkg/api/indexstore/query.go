package indexstore

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

const (
	// DefaultQueryLimit is the default number of rows returned.
	DefaultQueryLimit = 100
	// MaxQueryLimit is the maximum number of rows a client may request.
	MaxQueryLimit = 1000
)

// validOperators maps PostgREST-style operators to SQL fragments.
// The "is" operator is handled separately via a switch statement.
var validOperators = map[string]string{
	"eq":   "= ?",
	"neq":  "!= ?",
	"gt":   "> ?",
	"gte":  ">= ?",
	"lt":   "< ?",
	"lte":  "<= ?",
	"like": "LIKE ?",
	"in":   "IN ?",
}

// allowedRunColumns lists columns that may be filtered, sorted, or selected
// on the runs table. StepsJSON is excluded from filtering/sorting but
// included in the response DTO.
var allowedRunColumns = map[string]bool{
	"id":                 true,
	"discovery_path":     true,
	"run_id":             true,
	"timestamp":          true,
	"timestamp_end":      true,
	"suite_hash":         true,
	"status":             true,
	"termination_reason": true,
	"has_result":         true,
	"instance_id":        true,
	"client":             true,
	"image":              true,
	"rollback_strategy":  true,
	"tests_total":        true,
	"tests_passed":       true,
	"tests_failed":       true,
	"indexed_at":         true,
	"reindexed_at":       true,
}

// allowedTestStatColumns lists columns that may be filtered, sorted, or
// selected on the test_stats table.
var allowedTestStatColumns = map[string]bool{
	"id":             true,
	"suite_hash":     true,
	"test_name":      true,
	"run_id":         true,
	"client":         true,
	"total_gas_used": true,
	"total_time_ns":  true,
	"total_mgas_s":   true,
	"setup_gas_used": true,
	"setup_time_ns":  true,
	"setup_mgas_s":   true,
	"test_gas_used":  true,
	"test_time_ns":   true,
	"test_mgas_s":    true,
	"run_start":      true,
	"run_end":        true,
	// Setup resource stats.
	"setup_rpc_calls_count":             true,
	"setup_resource_cpu_usec":           true,
	"setup_resource_memory_delta_bytes": true,
	"setup_resource_memory_bytes":       true,
	"setup_resource_disk_read_bytes":    true,
	"setup_resource_disk_write_bytes":   true,
	"setup_resource_disk_read_iops":     true,
	"setup_resource_disk_write_iops":    true,
	// Test resource stats.
	"test_rpc_calls_count":             true,
	"test_resource_cpu_usec":           true,
	"test_resource_memory_delta_bytes": true,
	"test_resource_memory_bytes":       true,
	"test_resource_disk_read_bytes":    true,
	"test_resource_disk_write_bytes":   true,
	"test_resource_disk_read_iops":     true,
	"test_resource_disk_write_iops":    true,
}

// allowedTestStatsBlockLogColumns lists columns that may be filtered, sorted,
// or selected on the test_stats_block_logs table.
var allowedTestStatsBlockLogColumns = map[string]bool{
	"id":                           true,
	"suite_hash":                   true,
	"run_id":                       true,
	"test_name":                    true,
	"client":                       true,
	"block_number":                 true,
	"block_hash":                   true,
	"block_gas_used":               true,
	"block_tx_count":               true,
	"timing_execution_ms":          true,
	"timing_state_read_ms":         true,
	"timing_state_hash_ms":         true,
	"timing_commit_ms":             true,
	"timing_total_ms":              true,
	"throughput_mgas_per_sec":      true,
	"state_read_accounts":          true,
	"state_read_storage_slots":     true,
	"state_read_code":              true,
	"state_read_code_bytes":        true,
	"state_write_accounts":         true,
	"state_write_accounts_deleted": true,
	"state_write_storage_slots":    true,
	"state_write_slots_deleted":    true,
	"state_write_code":             true,
	"state_write_code_bytes":       true,
	"cache_account_hits":           true,
	"cache_account_misses":         true,
	"cache_account_hit_rate":       true,
	"cache_storage_hits":           true,
	"cache_storage_misses":         true,
	"cache_storage_hit_rate":       true,
	"cache_code_hits":              true,
	"cache_code_misses":            true,
	"cache_code_hit_rate":          true,
	"cache_code_hit_bytes":         true,
	"cache_code_miss_bytes":        true,
}

// Filter represents a single column filter.
type Filter struct {
	Column   string
	Operator string
	Value    string
}

// Order represents a single sort directive.
type Order struct {
	Column    string
	Direction string // "asc" or "desc"
}

// QueryParams holds the validated, parsed query parameters.
type QueryParams struct {
	Filters []Filter
	Orders  []Order
	Limit   int
	Offset  int
	Select  []string
}

// QueryResult wraps the paginated response.
type QueryResult struct {
	Data   any   `json:"data"`
	Total  int64 `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

// RunResponse is the JSON DTO for a runs row.
type RunResponse struct {
	ID                uint            `json:"id"`
	DiscoveryPath     string          `json:"discovery_path"`
	RunID             string          `json:"run_id"`
	Timestamp         int64           `json:"timestamp"`
	TimestampEnd      int64           `json:"timestamp_end"`
	SuiteHash         string          `json:"suite_hash"`
	Status            string          `json:"status"`
	TerminationReason string          `json:"termination_reason"`
	HasResult         bool            `json:"has_result"`
	InstanceID        string          `json:"instance_id"`
	Client            string          `json:"client"`
	Image             string          `json:"image"`
	RollbackStrategy  string          `json:"rollback_strategy"`
	TestsTotal        int             `json:"tests_total"`
	TestsPassed       int             `json:"tests_passed"`
	TestsFailed       int             `json:"tests_failed"`
	StepsJSON         json.RawMessage `json:"steps_json,omitempty"`
	IndexedAt         string          `json:"indexed_at"`
	ReindexedAt       *string         `json:"reindexed_at,omitempty"`
}

// TestStatResponse is the JSON DTO for a test_stats row.
type TestStatResponse struct {
	ID        uint   `json:"id"`
	SuiteHash string `json:"suite_hash"`
	RunID     string `json:"run_id"`
	TestName  string `json:"test_name"`
	Client    string `json:"client"`
	RunStart  int64  `json:"run_start"`
	RunEnd    int64  `json:"run_end"`

	TotalGasUsed uint64  `json:"total_gas_used"`
	TotalTimeNs  int64   `json:"total_time_ns"`
	TotalMGasS   float64 `json:"total_mgas_s"`

	SetupGasUsed              uint64  `json:"setup_gas_used"`
	SetupTimeNs               int64   `json:"setup_time_ns"`
	SetupMGasS                float64 `json:"setup_mgas_s"`
	SetupRPCCallsCount        int     `json:"setup_rpc_calls_count"`
	SetupResourceCPUUsec      uint64  `json:"setup_resource_cpu_usec"`
	SetupResourceMemDelta     int64   `json:"setup_resource_memory_delta_bytes"`
	SetupResourceMemBytes     uint64  `json:"setup_resource_memory_bytes"`
	SetupResourceDiskReadB    uint64  `json:"setup_resource_disk_read_bytes"`
	SetupResourceDiskWriteB   uint64  `json:"setup_resource_disk_write_bytes"`
	SetupResourceDiskReadOps  uint64  `json:"setup_resource_disk_read_iops"`
	SetupResourceDiskWriteOps uint64  `json:"setup_resource_disk_write_iops"`

	TestGasUsed              uint64  `json:"test_gas_used"`
	TestTimeNs               int64   `json:"test_time_ns"`
	TestMGasS                float64 `json:"test_mgas_s"`
	TestRPCCallsCount        int     `json:"test_rpc_calls_count"`
	TestResourceCPUUsec      uint64  `json:"test_resource_cpu_usec"`
	TestResourceMemDelta     int64   `json:"test_resource_memory_delta_bytes"`
	TestResourceMemBytes     uint64  `json:"test_resource_memory_bytes"`
	TestResourceDiskReadB    uint64  `json:"test_resource_disk_read_bytes"`
	TestResourceDiskWriteB   uint64  `json:"test_resource_disk_write_bytes"`
	TestResourceDiskReadOps  uint64  `json:"test_resource_disk_read_iops"`
	TestResourceDiskWriteOps uint64  `json:"test_resource_disk_write_iops"`

	StepsJSON json.RawMessage `json:"steps_json,omitempty"`
}

// TestStatsBlockLogResponse is the JSON DTO for a test_stats_block_logs row.
type TestStatsBlockLogResponse struct {
	ID        uint   `json:"id"`
	SuiteHash string `json:"suite_hash"`
	RunID     string `json:"run_id"`
	TestName  string `json:"test_name"`
	Client    string `json:"client"`

	BlockNumber  uint64 `json:"block_number"`
	BlockHash    string `json:"block_hash"`
	BlockGasUsed uint64 `json:"block_gas_used"`
	BlockTxCount int    `json:"block_tx_count"`

	TimingExecutionMs float64 `json:"timing_execution_ms"`
	TimingStateReadMs float64 `json:"timing_state_read_ms"`
	TimingStateHashMs float64 `json:"timing_state_hash_ms"`
	TimingCommitMs    float64 `json:"timing_commit_ms"`
	TimingTotalMs     float64 `json:"timing_total_ms"`

	ThroughputMgasPerSec float64 `json:"throughput_mgas_per_sec"`

	StateReadAccounts     int `json:"state_read_accounts"`
	StateReadStorageSlots int `json:"state_read_storage_slots"`
	StateReadCode         int `json:"state_read_code"`
	StateReadCodeBytes    int `json:"state_read_code_bytes"`

	StateWriteAccounts        int `json:"state_write_accounts"`
	StateWriteAccountsDeleted int `json:"state_write_accounts_deleted"`
	StateWriteStorageSlots    int `json:"state_write_storage_slots"`
	StateWriteSlotsDeleted    int `json:"state_write_slots_deleted"`
	StateWriteCode            int `json:"state_write_code"`
	StateWriteCodeBytes       int `json:"state_write_code_bytes"`

	CacheAccountHits    int     `json:"cache_account_hits"`
	CacheAccountMisses  int     `json:"cache_account_misses"`
	CacheAccountHitRate float64 `json:"cache_account_hit_rate"`

	CacheStorageHits    int     `json:"cache_storage_hits"`
	CacheStorageMisses  int     `json:"cache_storage_misses"`
	CacheStorageHitRate float64 `json:"cache_storage_hit_rate"`

	CacheCodeHits      int     `json:"cache_code_hits"`
	CacheCodeMisses    int     `json:"cache_code_misses"`
	CacheCodeHitRate   float64 `json:"cache_code_hit_rate"`
	CacheCodeHitBytes  int     `json:"cache_code_hit_bytes"`
	CacheCodeMissBytes int     `json:"cache_code_miss_bytes"`
}

// AllowedRunColumns returns the set of queryable run columns.
func AllowedRunColumns() map[string]bool {
	return allowedRunColumns
}

// AllowedTestStatColumns returns the set of queryable test stat columns.
func AllowedTestStatColumns() map[string]bool {
	return allowedTestStatColumns
}

// AllowedTestStatsBlockLogColumns returns the set of queryable test stats
// block log columns.
func AllowedTestStatsBlockLogColumns() map[string]bool {
	return allowedTestStatsBlockLogColumns
}

// ParseQueryParams validates and parses raw URL query values against the
// provided column whitelist. It returns an error for any invalid column,
// operator, or parameter value.
func ParseQueryParams(
	raw url.Values, allowedCols map[string]bool,
) (*QueryParams, error) {
	params := &QueryParams{
		Limit:  DefaultQueryLimit,
		Offset: 0,
	}

	// Parse limit.
	if v := raw.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("invalid limit: %s", v)
		}

		if n > MaxQueryLimit {
			n = MaxQueryLimit
		}

		params.Limit = n
	}

	// Parse offset.
	if v := raw.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("invalid offset: %s", v)
		}

		params.Offset = n
	}

	// Parse select.
	if v := raw.Get("select"); v != "" {
		cols := strings.Split(v, ",")
		for _, col := range cols {
			col = strings.TrimSpace(col)
			if col == "" {
				continue
			}

			if !allowedCols[col] {
				return nil, fmt.Errorf("invalid select column: %s", col)
			}

			params.Select = append(params.Select, col)
		}
	}

	// Parse order.
	if v := raw.Get("order"); v != "" {
		parts := strings.Split(v, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}

			segments := strings.SplitN(part, ".", 2)
			if len(segments) != 2 {
				return nil, fmt.Errorf("invalid order format: %s", part)
			}

			col := segments[0]
			dir := strings.ToLower(segments[1])

			if !allowedCols[col] {
				return nil, fmt.Errorf("invalid order column: %s", col)
			}

			if dir != "asc" && dir != "desc" {
				return nil, fmt.Errorf(
					"invalid order direction: %s (must be asc or desc)", dir,
				)
			}

			params.Orders = append(params.Orders, Order{
				Column:    col,
				Direction: dir,
			})
		}
	}

	// Parse filters: any key not in the reserved set is treated as a
	// column filter in the form column=operator.value.
	reserved := map[string]bool{
		"limit": true, "offset": true,
		"select": true, "order": true,
	}

	for key, values := range raw {
		if reserved[key] {
			continue
		}

		if !allowedCols[key] {
			return nil, fmt.Errorf("invalid filter column: %s", key)
		}

		for _, v := range values {
			dotIdx := strings.Index(v, ".")
			if dotIdx < 0 {
				return nil, fmt.Errorf(
					"invalid filter format for %s: %s "+
						"(expected operator.value)", key, v,
				)
			}

			op := v[:dotIdx]
			val := v[dotIdx+1:]

			if op == "is" {
				switch val {
				case "null", "true", "false":
					// ok
				default:
					return nil, fmt.Errorf(
						"invalid is value for %s: %s "+
							"(must be null, true, or false)", key, val,
					)
				}
			} else if _, ok := validOperators[op]; !ok {
				return nil, fmt.Errorf("invalid operator: %s", op)
			}

			params.Filters = append(params.Filters, Filter{
				Column:   key,
				Operator: op,
				Value:    val,
			})
		}
	}

	return params, nil
}

// applyQuery builds a GORM query chain from validated QueryParams.
func applyQuery(
	db *gorm.DB, model any, params *QueryParams,
) *gorm.DB {
	q := db.Model(model)

	// Apply select.
	if len(params.Select) > 0 {
		q = q.Select(params.Select)
	}

	// Apply filters.
	for _, f := range params.Filters {
		q = applyFilter(q, f)
	}

	// Apply order.
	for _, o := range params.Orders {
		q = q.Order(fmt.Sprintf("%s %s", o.Column, o.Direction))
	}

	return q
}

// applyFilter applies a single filter to the GORM chain.
func applyFilter(db *gorm.DB, f Filter) *gorm.DB {
	if f.Operator == "is" {
		return applyIsFilter(db, f)
	}

	if f.Operator == "in" {
		values := strings.Split(f.Value, ",")
		return db.Where(
			fmt.Sprintf("%s IN ?", f.Column), values,
		)
	}

	sqlOp := validOperators[f.Operator]

	return db.Where(
		fmt.Sprintf("%s %s", f.Column, sqlOp), f.Value,
	)
}

// applyIsFilter handles the special "is" operator for null/true/false.
func applyIsFilter(db *gorm.DB, f Filter) *gorm.DB {
	switch f.Value {
	case "null":
		return db.Where(fmt.Sprintf("%s IS NULL", f.Column))
	case "true":
		return db.Where(fmt.Sprintf("%s = ?", f.Column), true)
	case "false":
		return db.Where(fmt.Sprintf("%s = ?", f.Column), false)
	default:
		return db
	}
}

// toRunResponse converts a Run model to its JSON DTO.
func toRunResponse(r *Run) RunResponse {
	resp := RunResponse{
		ID:                r.ID,
		DiscoveryPath:     r.DiscoveryPath,
		RunID:             r.RunID,
		Timestamp:         r.Timestamp,
		TimestampEnd:      r.TimestampEnd,
		SuiteHash:         r.SuiteHash,
		Status:            r.Status,
		TerminationReason: r.TerminationReason,
		HasResult:         r.HasResult,
		InstanceID:        r.InstanceID,
		Client:            r.Client,
		Image:             r.Image,
		RollbackStrategy:  r.RollbackStrategy,
		TestsTotal:        r.TestsTotal,
		TestsPassed:       r.TestsPassed,
		TestsFailed:       r.TestsFailed,
		IndexedAt:         r.IndexedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}

	if r.ReindexedAt != nil {
		s := r.ReindexedAt.UTC().Format("2006-01-02T15:04:05Z")
		resp.ReindexedAt = &s
	}

	if r.StepsJSON != "" {
		resp.StepsJSON = json.RawMessage(r.StepsJSON)
	}

	return resp
}

// toTestStatResponse converts a TestStat model to its JSON DTO.
func toTestStatResponse(d *TestStat) TestStatResponse {
	resp := TestStatResponse{
		ID:           d.ID,
		SuiteHash:    d.SuiteHash,
		RunID:        d.RunID,
		TestName:     d.TestName,
		Client:       d.Client,
		RunStart:     d.RunStart,
		RunEnd:       d.RunEnd,
		TotalGasUsed: d.TotalGasUsed,
		TotalTimeNs:  d.TotalTimeNs,
		TotalMGasS:   d.TotalMGasS,
		// Setup step.
		SetupGasUsed:              d.SetupGasUsed,
		SetupTimeNs:               d.SetupTimeNs,
		SetupMGasS:                d.SetupMGasS,
		SetupRPCCallsCount:        d.SetupRPCCallsCount,
		SetupResourceCPUUsec:      d.SetupResourceCPUUsec,
		SetupResourceMemDelta:     d.SetupResourceMemDelta,
		SetupResourceMemBytes:     d.SetupResourceMemBytes,
		SetupResourceDiskReadB:    d.SetupResourceDiskReadB,
		SetupResourceDiskWriteB:   d.SetupResourceDiskWriteB,
		SetupResourceDiskReadOps:  d.SetupResourceDiskReadOps,
		SetupResourceDiskWriteOps: d.SetupResourceDiskWriteOps,
		// Test step.
		TestGasUsed:              d.TestGasUsed,
		TestTimeNs:               d.TestTimeNs,
		TestMGasS:                d.TestMGasS,
		TestRPCCallsCount:        d.TestRPCCallsCount,
		TestResourceCPUUsec:      d.TestResourceCPUUsec,
		TestResourceMemDelta:     d.TestResourceMemDelta,
		TestResourceMemBytes:     d.TestResourceMemBytes,
		TestResourceDiskReadB:    d.TestResourceDiskReadB,
		TestResourceDiskWriteB:   d.TestResourceDiskWriteB,
		TestResourceDiskReadOps:  d.TestResourceDiskReadOps,
		TestResourceDiskWriteOps: d.TestResourceDiskWriteOps,
	}

	if d.StepsJSON != "" {
		resp.StepsJSON = json.RawMessage(d.StepsJSON)
	}

	return resp
}

// toTestStatsBlockLogResponse converts a TestStatsBlockLog model to its
// JSON DTO.
func toTestStatsBlockLogResponse(l *TestStatsBlockLog) TestStatsBlockLogResponse {
	return TestStatsBlockLogResponse{
		ID:                        l.ID,
		SuiteHash:                 l.SuiteHash,
		RunID:                     l.RunID,
		TestName:                  l.TestName,
		Client:                    l.Client,
		BlockNumber:               l.BlockNumber,
		BlockHash:                 l.BlockHash,
		BlockGasUsed:              l.BlockGasUsed,
		BlockTxCount:              l.BlockTxCount,
		TimingExecutionMs:         l.TimingExecutionMs,
		TimingStateReadMs:         l.TimingStateReadMs,
		TimingStateHashMs:         l.TimingStateHashMs,
		TimingCommitMs:            l.TimingCommitMs,
		TimingTotalMs:             l.TimingTotalMs,
		ThroughputMgasPerSec:      l.ThroughputMgasPerSec,
		StateReadAccounts:         l.StateReadAccounts,
		StateReadStorageSlots:     l.StateReadStorageSlots,
		StateReadCode:             l.StateReadCode,
		StateReadCodeBytes:        l.StateReadCodeBytes,
		StateWriteAccounts:        l.StateWriteAccounts,
		StateWriteAccountsDeleted: l.StateWriteAccountsDeleted,
		StateWriteStorageSlots:    l.StateWriteStorageSlots,
		StateWriteSlotsDeleted:    l.StateWriteSlotsDeleted,
		StateWriteCode:            l.StateWriteCode,
		StateWriteCodeBytes:       l.StateWriteCodeBytes,
		CacheAccountHits:          l.CacheAccountHits,
		CacheAccountMisses:        l.CacheAccountMisses,
		CacheAccountHitRate:       l.CacheAccountHitRate,
		CacheStorageHits:          l.CacheStorageHits,
		CacheStorageMisses:        l.CacheStorageMisses,
		CacheStorageHitRate:       l.CacheStorageHitRate,
		CacheCodeHits:             l.CacheCodeHits,
		CacheCodeMisses:           l.CacheCodeMisses,
		CacheCodeHitRate:          l.CacheCodeHitRate,
		CacheCodeHitBytes:         l.CacheCodeHitBytes,
		CacheCodeMissBytes:        l.CacheCodeMissBytes,
	}
}
