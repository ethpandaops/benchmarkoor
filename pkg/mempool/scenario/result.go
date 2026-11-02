package scenario

// Status is the outcome of one scenario.
type Status string

const (
	// StatusPassed means every hard assertion held.
	StatusPassed Status = "passed"
	// StatusFailed means at least one hard assertion did not hold.
	StatusFailed Status = "failed"
	// StatusError means the scenario could not finish (RPC failure, timeout).
	StatusError Status = "error"
)

// Severity tells if an assertion can fail a scenario.
type Severity string

const (
	// SeverityHard fails the scenario when the assertion does not hold.
	SeverityHard Severity = "hard"
	// SeveritySoft records a behaviour. A difference between clients is a
	// finding, not a failure, because most pool rules are not consensus rules.
	SeveritySoft Severity = "soft"
)

// Result is the record of one scenario run against one client. The executor
// writes it to runs/<run>/<scenario>/mempool.result.json, and collects all of
// them in runs/<run>/mempool-result.json.
type Result struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Params      map[string]any `json:"params,omitempty"`
	Status      Status         `json:"status"`
	Error       string         `json:"error,omitempty"`
	// StartTime is the scenario start, in unix milliseconds.
	StartTime int64 `json:"start_time"`
	// DurationMs is the wall time of the scenario.
	DurationMs float64 `json:"duration_ms"`

	Metrics      []Metric      `json:"metrics"`
	Latencies    []Latency     `json:"latencies"`
	Timeline     []PoolSample  `json:"timeline"`
	Events       []Event       `json:"events"`
	Assertions   []Assertion   `json:"assertions"`
	Observations []Observation `json:"observations"`
}

// Metric is one number that a scenario measured.
type Metric struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Unit  string  `json:"unit"`
	Value float64 `json:"value"`
	// Group puts related metrics together in the UI (e.g. "legacy", "blob").
	Group string `json:"group,omitempty"`
	// Better is "lower" or "higher". The UI uses it to color comparisons.
	Better string `json:"better,omitempty"`
}

// Latency is the distribution of one repeated measurement.
type Latency struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Unit  string  `json:"unit"` // always "ms"
	Group string  `json:"group,omitempty"`
	Count int     `json:"count"`
	Min   float64 `json:"min"`
	Mean  float64 `json:"mean"`
	P50   float64 `json:"p50"`
	P90   float64 `json:"p90"`
	P99   float64 `json:"p99"`
	Max   float64 `json:"max"`
	// Histogram has cumulative-free buckets: Count is the number of samples
	// in (previous Le, Le].
	Histogram []Bucket `json:"histogram"`
}

// Bucket is one histogram bucket.
type Bucket struct {
	Le    float64 `json:"le"`
	Count int     `json:"count"`
}

// PoolSample is one poll of the pool and the container.
type PoolSample struct {
	// TMs is the time since the scenario start, in milliseconds.
	TMs     float64 `json:"t_ms"`
	Pending int     `json:"pending"`
	Queued  int     `json:"queued"`
	// MemoryBytes is the container memory use. Zero when not available.
	MemoryBytes uint64 `json:"memory_bytes,omitempty"`
	// CPUPercent is the container CPU use (100 = one core). Absent when not
	// available.
	CPUPercent *float64 `json:"cpu_percent,omitempty"`
}

// Event marks one action of the driver on the timeline.
type Event struct {
	TMs    float64 `json:"t_ms"`
	Kind   string  `json:"kind"`
	Detail string  `json:"detail,omitempty"`
}

// Assertion is one checked expectation.
type Assertion struct {
	Name     string   `json:"name"`
	Severity Severity `json:"severity"`
	Passed   bool     `json:"passed"`
	Expected string   `json:"expected,omitempty"`
	Actual   string   `json:"actual,omitempty"`
	// Explain says what the check tests, why, and which results are fine.
	Explain *Explanation `json:"explain,omitempty"`
}

// Observation is a behaviour that has no correct value, for example the error
// text of a rejected transaction. The UI compares them between clients.
type Observation struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Value string `json:"value"`
}
