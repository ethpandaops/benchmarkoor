import { type IndexStepType, type TestHistoryDuration, getRunDurationAggregatedStats } from '@/api/types'
import { formatDuration } from '@/utils/format'

export type HistoryMetric = 'mgas' | 'duration' | 'rejected'

export const HISTORY_METRIC_LABELS: Record<HistoryMetric, string> = {
  mgas: 'MGas/s',
  duration: 'Duration',
  rejected: 'Reject time',
}

// The summary finds the latest step of each client in its last runs: the run
// where its level changed. It compares the median since the step with the
// median of up to BASELINE_RUNS runs before it. The API uses the same rule for
// the suite changes (pkg/api/changes.go), so change it in both places.
export const BASELINE_RUNS = 10
// Below this many runs before a step the median is too noisy to compare with.
export const MIN_BASELINE_RUNS = 3
// A split is a real step when the change is at least this many percent and at
// least STEP_NOISE_FACTOR times the normal spread.
const MIN_STEP_CHANGE = 5
const STEP_NOISE_FACTOR = 3
// The number of recent runs per client that the step search looks at. A larger
// window finds older steps.
export const CHANGE_WINDOW_OPTIONS = [30, 50, 100] as const
export const DEFAULT_CHANGE_WINDOW = 30

export interface HistoryPoint {
  runId: string
  client: string
  image?: string
  // Unix seconds.
  runStart: number
  // MGas/s, or nanoseconds for the duration and reject time.
  value: number
}

export interface ClientHistorySummary {
  client: string
  count: number
  latest: HistoryPoint
  median: number
  min: number
  max: number
  // The latest step of the client. Undefined when it has too few runs.
  baseline?: StepComparison
  // The first run since the step. Undefined like the baseline.
  since?: HistoryPoint
}

export interface BaselineComparison {
  // Median of the runs before the step.
  median: number
  // Number of runs before the step in the baseline.
  runs: number
  // Change of the median since the step against the median before it, in
  // percent.
  change: number
  // Normal spread in percent of the median: the median absolute deviation
  // scaled to a standard deviation.
  noise: number
}

export interface StepComparison extends BaselineComparison {
  // Index of the first point since the step.
  stepIndex: number
  // Number of points since the step, the latest one included.
  stepRuns: number
  // Median value since the step.
  stepMedian: number
}

// For MGas/s a higher value is better. For the times a lower value is better.
export function isHigherBetter(metric: HistoryMetric): boolean {
  return metric === 'mgas'
}

function rejectedTimeNs(d: TestHistoryDuration, stepFilter: IndexStepType[]): number {
  if (!d.steps) return d.rejected_time_ns ?? 0

  let total = 0
  for (const step of stepFilter) {
    total += d.steps[step]?.rejected_time_ns ?? 0
  }
  return total
}

// Returns the metric of one run, or undefined when the run has no value for it
// (for example no gas in the selected steps).
export function historyMetricValue(
  d: TestHistoryDuration,
  metric: HistoryMetric,
  stepFilter: IndexStepType[],
): number | undefined {
  if (metric === 'rejected') {
    const ns = rejectedTimeNs(d, stepFilter)
    return ns > 0 ? ns : undefined
  }

  const { gasUsed, timeNs } = getRunDurationAggregatedStats(d, stepFilter)
  if (timeNs <= 0) return undefined
  if (metric === 'duration') return timeNs
  if (gasUsed <= 0) return undefined
  return (gasUsed * 1000) / timeNs
}

export function hasRejectedTime(durations: TestHistoryDuration[]): boolean {
  return durations.some((d) => (d.rejected_time_ns ?? 0) > 0)
}

// Groups the runs by client, oldest first, and drops runs with no value.
export function buildHistoryPoints(
  durations: TestHistoryDuration[],
  metric: HistoryMetric,
  stepFilter: IndexStepType[],
): Map<string, HistoryPoint[]> {
  const byClient = new Map<string, HistoryPoint[]>()

  for (const d of durations) {
    const value = historyMetricValue(d, metric, stepFilter)
    if (value === undefined) continue

    let points = byClient.get(d.client)
    if (!points) {
      points = []
      byClient.set(d.client, points)
    }
    points.push({ runId: d.id, client: d.client, image: d.image, runStart: d.run_start, value })
  }

  for (const points of byClient.values()) {
    points.sort((a, b) => a.runStart - b.runStart || a.runId.localeCompare(b.runId))
  }

  return new Map([...byClient.entries()].sort(([a], [b]) => a.localeCompare(b)))
}

export function median(values: number[]): number {
  if (values.length === 0) return NaN
  const sorted = [...values].sort((a, b) => a - b)
  const mid = Math.floor(sorted.length / 2)
  return sorted.length % 2 === 0 ? (sorted[mid - 1] + sorted[mid]) / 2 : sorted[mid]
}

// 1.4826 × MAD estimates the standard deviation of normally distributed data.
const MAD_TO_STDDEV = 1.4826

function sortedMedian(sorted: number[]): number {
  const mid = Math.floor(sorted.length / 2)
  return sorted.length % 2 === 0 ? (sorted[mid - 1] + sorted[mid]) / 2 : sorted[mid]
}

// For each length i, the sum of the absolute deviations of values[0..i) from
// their median. It sums in ascending order, as the API does.
function deviationCosts(values: number[]): number[] {
  const costs = [0]
  const sorted: number[] = []

  for (const v of values) {
    let pos = 0
    while (pos < sorted.length && sorted[pos] < v) pos++
    sorted.splice(pos, 0, v)

    const m = sortedMedian(sorted)
    let sum = 0
    for (const x of sorted) sum += Math.abs(x - m)
    costs.push(sum)
  }

  return costs
}

// Returns the split of values into a before and an after part that fits two
// levels best, with at least MIN_BASELINE_RUNS values before it. A tie goes to
// the later split. Returns -1 when values is too short.
function bestSplit(values: number[]): number {
  const n = values.length
  if (n < MIN_BASELINE_RUNS + 1) return -1

  const before = deviationCosts(values)
  const after = deviationCosts([...values].reverse())

  let best = -1
  let bestCost = Infinity
  for (let k = MIN_BASELINE_RUNS; k < n; k++) {
    const cost = before[k] + after[n - k]
    if (cost <= bestCost) {
      best = k
      bestCost = cost
    }
  }
  return best
}

// Compares the median of after with the median of the last BASELINE_RUNS
// values of before. The spread comes from both sides once after has enough
// values, because the before side alone can look calm by chance.
function levelChange(before: number[], after: number[]): BaselineComparison | undefined {
  const baseline = before.slice(-BASELINE_RUNS)
  const baselineMedian = median(baseline)
  if (!(baselineMedian > 0)) return undefined

  const afterMedian = median(after)
  const deviations = baseline.map((v) => Math.abs(v - baselineMedian))
  if (after.length >= MIN_BASELINE_RUNS) {
    for (const v of after) deviations.push((Math.abs(v - afterMedian) * baselineMedian) / afterMedian)
  }

  return {
    median: baselineMedian,
    runs: baseline.length,
    change: ((afterMedian - baselineMedian) / baselineMedian) * 100,
    noise: ((median(deviations) * MAD_TO_STDDEV) / baselineMedian) * 100,
  }
}

// A split of a series into before and after is a real step when the change is
// at least MIN_STEP_CHANGE percent and at least STEP_NOISE_FACTOR times the
// normal spread.
function isStep(before: number[], after: number[]): boolean {
  const c = levelChange(before, after)
  return !!c && Math.abs(c.change) >= Math.max(MIN_STEP_CHANGE, STEP_NOISE_FACTOR * c.noise)
}

// Returns the start of the level that ends at end: the latest real step in
// values[from..end) with at least MIN_BASELINE_RUNS values after it, or from. A
// before part that spans two levels has a large spread, and a real step after
// it then looks like noise.
function levelStart(values: number[], from: number, end: number): number {
  for (;;) {
    const split = bestSplit(values.slice(from, end))
    if (split < 0 || end - (from + split) < MIN_BASELINE_RUNS) return from
    if (!isStep(values.slice(from, from + split), values.slice(from + split, end))) return from
    from += split
  }
}

// Returns the start of the latest level of a series and the start of the level
// before it. It splits the series where it fits two levels best, and splits
// the after part again while each split is a real step. The before part of a
// split starts at its own latest step. With no real step, the latest value is
// its own level.
function findStep(values: number[]): { step: number; start: number } {
  let step = values.length - 1
  let start = 0

  for (let from = 0; ; ) {
    const split = bestSplit(values.slice(from))
    if (split < 0) return { step, start }

    const k = split + from
    const levelFrom = levelStart(values, from, k)
    if (!isStep(values.slice(levelFrom, k), values.slice(k))) return { step, start }

    step = k
    start = levelFrom
    from = k
  }
}

// Finds the latest step in the last `window` points and compares the points
// since the step with the points before it. The points must be oldest first.
// Returns undefined when there are too few points.
export function compareWithBaseline(
  points: HistoryPoint[],
  window: number = DEFAULT_CHANGE_WINDOW,
): StepComparison | undefined {
  const recent = points.slice(-window)
  if (recent.length < MIN_BASELINE_RUNS + 1) return undefined

  const values = recent.map((p) => p.value)
  const { step, start } = findStep(values)
  const baseline = levelChange(values.slice(start, step), values.slice(step))
  if (!baseline) return undefined

  return {
    ...baseline,
    stepIndex: points.length - recent.length + step,
    stepRuns: recent.length - step,
    stepMedian: median(values.slice(step)),
  }
}

// Summarizes the points of one client. The points must be oldest first.
export function summarizeClient(
  client: string,
  points: HistoryPoint[],
  window: number = DEFAULT_CHANGE_WINDOW,
): ClientHistorySummary | undefined {
  if (points.length === 0) return undefined

  const values = points.map((p) => p.value)
  const baseline = compareWithBaseline(points, window)

  return {
    client,
    count: points.length,
    latest: points[points.length - 1],
    median: median(values),
    min: Math.min(...values),
    max: Math.max(...values),
    baseline,
    since: baseline && points[baseline.stepIndex],
  }
}

// A gap between two runs breaks the chart line when it is GAP_FACTOR times the
// median interval of the client, and at least MIN_GAP_SECONDS. A line across
// a gap of days looks like a slow trend, but there are no runs in it.
const GAP_FACTOR = 5
const MIN_GAP_SECONDS = 6 * 3600

// Returns the indexes of the points that start a new line after a gap. The
// run starts must be in Unix seconds, oldest first.
export function findRunGaps(runStarts: number[]): Set<number> {
  const intervals = runStarts.slice(1).map((t, i) => t - runStarts[i])
  const gaps = new Set<number>()
  if (intervals.length < 2) return gaps

  const limit = Math.max(GAP_FACTOR * median(intervals), MIN_GAP_SECONDS)
  intervals.forEach((interval, i) => {
    if (interval > limit) gaps.add(i + 1)
  })
  return gaps
}

// Formats a metric value for display.
export function formatHistoryValue(value: number, metric: HistoryMetric): string {
  return metric === 'mgas' ? `${value.toFixed(2)} MGas/s` : formatDuration(Math.round(value))
}
