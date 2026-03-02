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

// allowedTestDurationColumns lists columns that may be filtered, sorted, or
// selected on the test_durations table.
var allowedTestDurationColumns = map[string]bool{
	"id":         true,
	"suite_hash": true,
	"test_name":  true,
	"run_id":     true,
	"client":     true,
	"gas_used":   true,
	"time_ns":    true,
	"run_start":  true,
	"run_end":    true,
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

// TestDurationResponse is the JSON DTO for a test_durations row.
type TestDurationResponse struct {
	ID        uint            `json:"id"`
	SuiteHash string          `json:"suite_hash"`
	TestName  string          `json:"test_name"`
	RunID     string          `json:"run_id"`
	Client    string          `json:"client"`
	GasUsed   uint64          `json:"gas_used"`
	TimeNs    int64           `json:"time_ns"`
	RunStart  int64           `json:"run_start"`
	RunEnd    int64           `json:"run_end"`
	StepsJSON json.RawMessage `json:"steps_json,omitempty"`
}

// AllowedRunColumns returns the set of queryable run columns.
func AllowedRunColumns() map[string]bool {
	return allowedRunColumns
}

// AllowedTestDurationColumns returns the set of queryable test duration
// columns.
func AllowedTestDurationColumns() map[string]bool {
	return allowedTestDurationColumns
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

// toTestDurationResponse converts a TestDuration model to its JSON DTO.
func toTestDurationResponse(d *TestDuration) TestDurationResponse {
	resp := TestDurationResponse{
		ID:        d.ID,
		SuiteHash: d.SuiteHash,
		TestName:  d.TestName,
		RunID:     d.RunID,
		Client:    d.Client,
		GasUsed:   d.GasUsed,
		TimeNs:    d.TimeNs,
		RunStart:  d.RunStart,
		RunEnd:    d.RunEnd,
	}

	if d.StepsJSON != "" {
		resp.StepsJSON = json.RawMessage(d.StepsJSON)
	}

	return resp
}
