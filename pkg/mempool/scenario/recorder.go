package scenario

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/ethpandaops/benchmarkoor/pkg/mempool/observer"
)

// Recorder collects what a scenario measures and checks. It is safe for
// concurrent use.
type Recorder struct {
	mu        sync.Mutex
	start     time.Time
	result    *Result
	latencies map[string]*latencySeries
	order     []string
	checked   map[string]bool
}

type latencySeries struct {
	label   string
	group   string
	samples []float64
}

// NewRecorder starts a record for one scenario.
func NewRecorder(name, description string, params Params) *Recorder {
	now := time.Now()

	return &Recorder{
		start: now,
		result: &Result{
			Name:         name,
			Description:  description,
			Params:       params,
			Status:       StatusPassed,
			StartTime:    now.UnixMilli(),
			Metrics:      make([]Metric, 0, 16),
			Latencies:    make([]Latency, 0, 8),
			Timeline:     make([]PoolSample, 0, 256),
			Events:       make([]Event, 0, 64),
			Assertions:   make([]Assertion, 0, 16),
			Observations: make([]Observation, 0, 8),
		},
		latencies: make(map[string]*latencySeries, 8),
		checked:   make(map[string]bool, 16),
	}
}

// Start is the scenario start time.
func (r *Recorder) Start() time.Time { return r.start }

func (r *Recorder) sinceStart(t time.Time) float64 {
	return float64(t.Sub(r.start).Microseconds()) / 1000
}

// Event marks a driver action on the timeline.
func (r *Recorder) Event(kind, format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.result.Events = append(r.result.Events, Event{
		TMs: r.sinceStart(time.Now()), Kind: kind, Detail: fmt.Sprintf(format, args...),
	})
}

// Metric records one number.
func (r *Recorder) Metric(m Metric) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.result.Metrics = append(r.result.Metrics, m)
}

// Latency adds one sample to a latency series.
func (r *Recorder) Latency(key, label, group string, d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.latencies[key]
	if !ok {
		s = &latencySeries{label: label, group: group, samples: make([]float64, 0, 64)}
		r.latencies[key] = s
		r.order = append(r.order, key)
	}

	s.samples = append(s.samples, float64(d.Microseconds())/1000)
}

// Check records an assertion. A hard assertion that does not hold fails the
// scenario. It returns ok.
func (r *Recorder) Check(sev Severity, ok bool, name, expected, actual string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.result.Assertions = append(r.result.Assertions, Assertion{
		Name: name, Severity: sev, Passed: ok, Expected: expected, Actual: actual,
	})

	if !ok && sev == SeverityHard && r.result.Status == StatusPassed {
		r.result.Status = StatusFailed
	}

	return ok
}

// CheckOnce is Check for a check that a scenario repeats (per round or per
// iteration): it records the first result and every failure, so the table
// lists each check once unless it fails.
func (r *Recorder) CheckOnce(sev Severity, ok bool, name, expected, actual string) bool {
	r.mu.Lock()
	seen := r.checked[name]
	r.checked[name] = true
	r.mu.Unlock()

	if seen && ok {
		return ok
	}

	return r.Check(sev, ok, name, expected, actual)
}

// Observe records a behaviour that has no correct value.
func (r *Recorder) Observe(key, label, value string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.result.Observations = append(r.result.Observations, Observation{
		Key: key, Label: label, Value: value,
	})
}

// Finish closes the record: it sets the status from err, converts the pool
// samples and computes the latency distributions.
func (r *Recorder) Finish(err error, samples []observer.Sample) *Result {
	r.mu.Lock()
	defer r.mu.Unlock()

	res := r.result
	res.DurationMs = r.sinceStart(time.Now())

	if err != nil {
		res.Status = StatusError
		res.Error = err.Error()
	}

	for _, s := range samples {
		if s.At.Before(r.start) {
			continue
		}

		p := PoolSample{
			TMs:         r.sinceStart(s.At),
			Pending:     s.Status.Pending,
			Queued:      s.Status.Queued,
			MemoryBytes: s.MemoryBytes,
		}

		if s.MemoryBytes > 0 {
			cpu := round3(s.CPUPercent)
			p.CPUPercent = &cpu
		}

		res.Timeline = append(res.Timeline, p)
	}

	for _, key := range r.order {
		s := r.latencies[key]
		res.Latencies = append(res.Latencies, summarize(key, s))
	}

	return res
}

// histogramBounds are the upper bounds of the latency buckets, in ms.
var histogramBounds = []float64{
	0.1, 0.25, 0.5, 1, 2.5, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000,
}

func summarize(key string, s *latencySeries) Latency {
	v := append([]float64(nil), s.samples...)
	sort.Float64s(v)

	l := Latency{Key: key, Label: s.label, Unit: "ms", Group: s.group, Count: len(v)}
	if len(v) == 0 {
		l.Histogram = []Bucket{}

		return l
	}

	sum := 0.0
	for _, x := range v {
		sum += x
	}

	l.Min = round3(v[0])
	l.Max = round3(v[len(v)-1])
	l.Mean = round3(sum / float64(len(v)))
	l.P50 = round3(percentile(v, 0.50))
	l.P90 = round3(percentile(v, 0.90))
	l.P99 = round3(percentile(v, 0.99))
	l.Histogram = histogram(v)

	return l
}

// percentile returns the nearest-rank percentile of sorted values.
func percentile(sorted []float64, p float64) float64 {
	rank := int(math.Ceil(p*float64(len(sorted)))) - 1
	rank = max(0, min(rank, len(sorted)-1))

	return sorted[rank]
}

// histogram returns non-cumulative buckets: Count is the number of samples in
// (previous Le, Le]. Empty buckets at both ends are left out. The last bucket
// has Le = +Inf written as the largest sample.
func histogram(sorted []float64) []Bucket {
	buckets := make([]Bucket, 0, len(histogramBounds)+1)
	i := 0

	for _, le := range histogramBounds {
		n := 0
		for i < len(sorted) && sorted[i] <= le {
			n++
			i++
		}

		buckets = append(buckets, Bucket{Le: le, Count: n})
	}

	if i < len(sorted) {
		buckets = append(buckets, Bucket{Le: round3(sorted[len(sorted)-1]), Count: len(sorted) - i})
	}

	first, last := 0, len(buckets)-1
	for first < last && buckets[first].Count == 0 {
		first++
	}

	for last > first && buckets[last].Count == 0 {
		last--
	}

	return buckets[first : last+1]
}

func round3(v float64) float64 {
	return math.Round(v*1000) / 1000
}

// Ms converts a duration to milliseconds for a Metric.
func Ms(d time.Duration) float64 {
	return round3(float64(d.Microseconds()) / 1000)
}
