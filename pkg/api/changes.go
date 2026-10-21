package api

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/ethpandaops/benchmarkoor/pkg/api/indexstore"
	"github.com/go-chi/chi/v5"
)

// The detection rule. The UI finds regressions with these values too (see
// ui/src/utils/testHistory.ts), so change them in both places.
const (
	// baselineRuns is the most runs before a step that the runs since the
	// step are compared with.
	baselineRuns = 10
	// minBaselineRuns is the fewest runs before a step that give a usable
	// median.
	minBaselineRuns = 3
	// madToStdDev scales the median absolute deviation to a standard
	// deviation for normally distributed data.
	madToStdDev = 1.4826
	// minStepChange and stepNoiseFactor decide if a split of a series is a
	// real step: the change must be at least minStepChange percent and at
	// least stepNoiseFactor times the normal spread of the runs before it.
	minStepChange   = 5
	stepNoiseFactor = 3
	// defaultChangeWindow is the number of recent runs per client that the
	// step search reads, when the request does not give one.
	defaultChangeWindow = 30
	// maxChangeCacheEntries caps the cached responses. A response for a
	// large suite is a few MB.
	maxChangeCacheEntries = 16
)

// changeWindows are the windows that a request can ask for. A larger window
// finds older steps, but reads more test stats.
var changeWindows = []int{30, 50, 100}

// changeMetric is the value that a test is compared on. Invalid-payload
// tests have no MGas/s, so they use the time to reject.
type changeMetric string

const (
	changeMetricMGas     changeMetric = "mgas"
	changeMetricRejected changeMetric = "rejected"
)

// changeStep is the latest level of a series: the runs since its last step.
type changeStep struct {
	// RunID and RunStart are the first run of the level.
	RunID    string `json:"run_id"`
	RunStart int64  `json:"run_start"`
	// Runs is the number of runs since the step, the latest run included.
	Runs int `json:"runs"`
	// Median is the median value of the runs since the step.
	Median float64 `json:"median"`
	// DurationNs is the median time of the selected steps since the step, or
	// 0 when no run has a time. For an invalid-payload test it is the time
	// to reject.
	DurationNs float64 `json:"duration_ns"`
}

type baselineComparison struct {
	// Median of the runs before the step.
	Median float64 `json:"median"`
	Runs   int     `json:"runs"`
	// Change of the median since the step against the median before it, in
	// percent.
	Change float64 `json:"change"`
	// Noise is the normal spread of the runs before the step, in percent of
	// their median.
	Noise float64 `json:"noise"`
	// DurationNs is the median time of the selected steps before the step,
	// or 0 when no run has a time. For an invalid-payload test it is the
	// time to reject.
	DurationNs float64 `json:"duration_ns"`
}

type clientChange struct {
	Client   string             `json:"client"`
	Step     changeStep         `json:"step"`
	Baseline baselineComparison `json:"baseline"`
}

type suiteTestChanges struct {
	Name    string         `json:"name"`
	Metric  changeMetric   `json:"metric"`
	Clients []clientChange `json:"clients"`
}

type suiteChangesResponse struct {
	Tests []suiteTestChanges `json:"tests"`
}

// changeStepFilter selects the steps whose gas and time count.
type changeStepFilter struct {
	setup, test bool
}

// parseChangeSteps parses a comma-separated step list. The test step is the
// default, as in the UI. The cleanup step has no test stats, so it is ignored.
func parseChangeSteps(param string) changeStepFilter {
	var steps changeStepFilter

	for _, step := range strings.Split(param, ",") {
		switch strings.TrimSpace(step) {
		case "setup":
			steps.setup = true
		case "test":
			steps.test = true
		}
	}

	if !steps.setup && !steps.test {
		steps.test = true
	}

	return steps
}

func (f changeStepFilter) String() string {
	switch {
	case f.setup && f.test:
		return "setup,test"
	case f.setup:
		return "setup"
	default:
		return "test"
	}
}

// totals returns the gas, time and time to reject of the selected steps.
func (f changeStepFilter) totals(
	st *indexstore.TestChangeStat,
) (gas uint64, timeNs, rejectedNs int64) {
	if f.setup {
		gas += st.SetupGasUsed
		timeNs += st.SetupTimeNs
		rejectedNs += st.SetupRejectedTimeNs
	}

	if f.test {
		gas += st.TestGasUsed
		timeNs += st.TestTimeNs
		rejectedNs += st.TestRejectedTimeNs
	}

	return gas, timeNs, rejectedNs
}

type seriesPoint struct {
	runID    string
	runStart int64
	value    float64
	// durationNs is the time of the selected steps, or the time to reject
	// for an invalid-payload test, or 0 when there is none.
	durationNs float64
}

func medianOf(values []float64) float64 {
	sorted := slices.Clone(values)
	slices.Sort(sorted)

	return sortedMedian(sorted)
}

func sortedMedian(sorted []float64) float64 {
	mid := len(sorted) / 2
	if len(sorted)%2 == 0 {
		return (sorted[mid-1] + sorted[mid]) / 2
	}

	return sorted[mid]
}

// sortedDeviation returns the sum of the absolute deviations from the median.
// It sums in ascending order, as the UI does, so both give the same float.
func sortedDeviation(sorted []float64) float64 {
	median := sortedMedian(sorted)

	var sum float64
	for _, v := range sorted {
		sum += math.Abs(v - median)
	}

	return sum
}

// deviationCosts returns, for each length i, the sum of the absolute
// deviations of values[:i] from their median.
func deviationCosts(values []float64) []float64 {
	costs := make([]float64, len(values)+1)
	sorted := make([]float64, 0, len(values))

	for i, v := range values {
		pos, _ := slices.BinarySearch(sorted, v)
		sorted = slices.Insert(sorted, pos, v)
		costs[i+1] = sortedDeviation(sorted)
	}

	return costs
}

// bestSplit returns the split of values into a before and an after part that
// fits two levels best: the sum of the absolute deviations from the median of
// each part is smallest. The before part has at least minBaselineRuns values.
// It returns -1 when values is too short. A tie goes to the later split.
func bestSplit(values []float64) int {
	n := len(values)
	if n < minBaselineRuns+1 {
		return -1
	}

	before := deviationCosts(values)

	reversed := slices.Clone(values)
	slices.Reverse(reversed)
	after := deviationCosts(reversed)

	best, bestCost := -1, math.Inf(1)

	for k := minBaselineRuns; k < n; k++ {
		if cost := before[k] + after[n-k]; cost <= bestCost {
			best, bestCost = k, cost
		}
	}

	return best
}

// levelChange compares the median of after with the median of the last
// baselineRuns values of before. It returns false when the median of before
// is not positive.
func levelChange(before, after []float64) (baselineComparison, bool) {
	before = before[max(0, len(before)-baselineRuns):]

	median := medianOf(before)
	if !(median > 0) {
		return baselineComparison{}, false
	}

	afterMedian := medianOf(after)

	// The spread comes from both sides, each around its own median, once
	// the after side has enough runs. The before side alone can look calm
	// by chance, and the step search would pick exactly such a split.
	deviations := make([]float64, 0, len(before)+len(after))
	for _, v := range before {
		deviations = append(deviations, math.Abs(v-median))
	}

	if len(after) >= minBaselineRuns {
		for _, v := range after {
			deviations = append(deviations, math.Abs(v-afterMedian)*median/afterMedian)
		}
	}

	return baselineComparison{
		Median: median,
		Runs:   len(before),
		Change: (afterMedian - median) / median * 100,
		Noise:  medianOf(deviations) * madToStdDev / median * 100,
	}, true
}

// findStep returns the start of the latest level of a series, and the start of
// the level before it. It splits the series where it fits two levels best,
// and splits the after part again while each split is a real step. With no
// real step, the latest value is its own level, as in a plain comparison of
// the latest run with the runs before it.
func findStep(values []float64) (step, start int) {
	step = len(values) - 1

	for from := 0; ; {
		k := bestSplit(values[from:])
		if k < 0 {
			return step, start
		}

		k += from

		c, ok := levelChange(values[from:k], values[k:])
		if !ok || math.Abs(c.Change) < max(minStepChange, stepNoiseFactor*c.Noise) {
			return step, start
		}

		step, start, from = k, from, k
	}
}

// compareWithBaseline finds the latest step of a series and compares the runs
// since the step with the runs before it. The points must be oldest first. It
// returns false when there are too few points.
func compareWithBaseline(points []seriesPoint) (changeStep, baselineComparison, bool) {
	if len(points) < minBaselineRuns+1 {
		return changeStep{}, baselineComparison{}, false
	}

	values := make([]float64, len(points))
	for i := range points {
		values[i] = points[i].value
	}

	step, start := findStep(values)

	baseline, ok := levelChange(values[start:step], values[step:])
	if !ok {
		return changeStep{}, baselineComparison{}, false
	}

	since := points[step:]
	baseline.DurationNs = medianDuration(points[step-baseline.Runs : step])

	return changeStep{
		RunID:      since[0].runID,
		RunStart:   since[0].runStart,
		Runs:       len(since),
		Median:     medianOf(values[step:]),
		DurationNs: medianDuration(since),
	}, baseline, true
}

// medianDuration returns the median time of the points that have one, or 0.
func medianDuration(points []seriesPoint) float64 {
	durations := make([]float64, 0, len(points))

	for i := range points {
		if points[i].durationNs > 0 {
			durations = append(durations, points[i].durationNs)
		}
	}

	if len(durations) == 0 {
		return 0
	}

	return medianOf(durations)
}

// sortPoints orders points oldest first, with the run ID as a tie-breaker.
func sortPoints(points []seriesPoint) {
	slices.SortFunc(points, func(a, b seriesPoint) int {
		return cmp.Or(cmp.Compare(a.runStart, b.runStart), strings.Compare(a.runID, b.runID))
	})
}

// changeSample is one run of a test, with the totals of the selected steps.
type changeSample struct {
	runID      string
	runStart   int64
	gas        uint64
	timeNs     int64
	rejectedNs int64
}

type testSamples struct {
	// rejected is true when any run has a time to reject, in any step.
	rejected bool
	clients  map[string][]changeSample
}

// changeBuilder collects the test stats of a suite one row at a time, so a
// large read keeps only the totals of the selected steps.
type changeBuilder struct {
	steps changeStepFilter
	tests map[string]*testSamples
}

func newChangeBuilder(steps changeStepFilter) *changeBuilder {
	return &changeBuilder{steps: steps, tests: make(map[string]*testSamples, 1024)}
}

func (b *changeBuilder) add(st *indexstore.TestChangeStat) {
	test, ok := b.tests[st.TestName]
	if !ok {
		test = &testSamples{clients: make(map[string][]changeSample, 8)}
		b.tests[st.TestName] = test
	}

	if st.SetupRejectedTimeNs+st.TestRejectedTimeNs > 0 {
		test.rejected = true
	}

	gas, timeNs, rejectedNs := b.steps.totals(st)
	test.clients[st.Client] = append(test.clients[st.Client],
		changeSample{st.RunID, st.RunStart, gas, timeNs, rejectedNs})
}

// build finds the latest step of each client and test, and compares the runs
// since the step with the runs before it.
func (b *changeBuilder) build() suiteChangesResponse {
	resp := suiteChangesResponse{Tests: make([]suiteTestChanges, 0, len(b.tests))}

	for name, samples := range b.tests {
		metric := changeMetricMGas
		if samples.rejected {
			metric = changeMetricRejected
		}

		test := suiteTestChanges{Name: name, Metric: metric}

		for client, runs := range samples.clients {
			points := make([]seriesPoint, 0, len(runs))

			for _, run := range runs {
				var value float64

				switch {
				case metric == changeMetricRejected && run.rejectedNs > 0:
					value = float64(run.rejectedNs)
				case metric == changeMetricMGas && run.timeNs > 0 && run.gas > 0:
					value = float64(run.gas) * 1000 / float64(run.timeNs)
				default:
					continue
				}

				// An invalid payload reports no gas, so its step time is 0. Its
				// time to reject is the time that the min. block time is
				// compared with.
				durationNs := run.timeNs
				if metric == changeMetricRejected {
					durationNs = run.rejectedNs
				}

				points = append(points,
					seriesPoint{run.runID, run.runStart, value, float64(max(durationNs, 0))})
			}

			sortPoints(points)

			step, baseline, ok := compareWithBaseline(points)
			if !ok {
				continue
			}

			test.Clients = append(test.Clients,
				clientChange{Client: client, Step: step, Baseline: baseline})
		}

		if len(test.Clients) == 0 {
			continue
		}

		slices.SortFunc(test.Clients, func(a, b clientChange) int {
			return strings.Compare(a.Client, b.Client)
		})

		resp.Tests = append(resp.Tests, test)
	}

	slices.SortFunc(resp.Tests, func(a, b suiteTestChanges) int {
		return strings.Compare(a.Name, b.Name)
	})

	return resp
}

// buildSuiteChanges builds the response from a slice of test stats.
func buildSuiteChanges(
	stats []indexstore.TestChangeStat, steps changeStepFilter,
) suiteChangesResponse {
	b := newChangeBuilder(steps)
	for i := range stats {
		b.add(&stats[i])
	}

	return b.build()
}

// parseChangeWindow returns the window of the request, or the default when
// the value is not one of changeWindows.
func parseChangeWindow(param string) int {
	window, err := strconv.Atoi(param)
	if err != nil || !slices.Contains(changeWindows, window) {
		return defaultChangeWindow
	}

	return window
}

// changeCacheKey identifies a response. The run IDs make the key change when
// a run gets its first test stats or loses them. The test stats generation
// makes it change when a re-index rewrites the stats of a run.
func changeCacheKey(
	suiteHash string, steps changeStepFilter, window int, gen uint64, runIDs []string,
) string {
	sum := sha256.Sum256([]byte(strings.Join(runIDs, "\x00")))

	return fmt.Sprintf("%s|%s|%d|%d|%s",
		suiteHash, steps, window, gen, hex.EncodeToString(sum[:]))
}

// handleSuiteChanges finds the latest step of each client and test of a
// suite in its recent runs. It is the data behind the regressions list:
// a compact summary instead of the full stats of every recent run.
func (s *server) handleSuiteChanges(w http.ResponseWriter, r *http.Request) {
	suiteHash := chi.URLParam(r, "hash")
	if suiteHash == "" {
		writeJSON(w, http.StatusBadRequest,
			errorResponse{"suite hash is required"})

		return
	}

	steps := parseChangeSteps(r.URL.Query().Get("steps"))
	window := parseChangeWindow(r.URL.Query().Get("window"))

	// Read the generation before the stats. A write between the two then
	// caches new stats under the old key, which is only a wasted entry.
	gen := s.indexStore.TestStatsGeneration(suiteHash)

	// The run list comes from the runs table, so it is cheap. It is the
	// cache key, and the input of the expensive test_stats read below.
	runIDs, err := s.indexStore.ListRecentRunIDs(r.Context(), suiteHash, window)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError,
			errorResponse{"listing runs: " + err.Error()})

		return
	}

	key := changeCacheKey(suiteHash, steps, window, gen, runIDs)

	s.changeCacheMu.Lock()
	entry, ok := s.changeCache[key]
	s.changeCacheMu.Unlock()

	if !ok {
		builder := newChangeBuilder(steps)
		if err := s.indexStore.ScanTestChangeStats(
			r.Context(), suiteHash, runIDs, builder.add,
		); err != nil {
			writeJSON(w, http.StatusInternalServerError,
				errorResponse{"listing test stats: " + err.Error()})

			return
		}

		body, err := json.Marshal(builder.build())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError,
				errorResponse{"encoding changes: " + err.Error()})

			return
		}

		// Gzip once here, not on every request: the body is a few MB for a
		// large suite, and the API runs on little CPU.
		entry = newIndexCacheEntry(s.log, 0, body)

		s.changeCacheMu.Lock()
		if s.changeCache == nil || len(s.changeCache) >= maxChangeCacheEntries {
			// Old keys are never asked for again, so a full cache starts over.
			s.changeCache = make(map[string]*indexCacheEntry, maxChangeCacheEntries)
		}

		s.changeCache[key] = entry
		s.changeCacheMu.Unlock()
	}

	// The same writer as /index: it serves the stored gzip copy when the
	// client accepts it, and answers a matching If-None-Match with a 304.
	writeIndexResponse(w, r, entry)
}
