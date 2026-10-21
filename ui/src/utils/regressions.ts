import {
  getRunDurationAggregatedStats,
  type BaselineStats,
  type ChangeStep,
  type ClientChange,
  type IndexStepType,
  type SuiteChanges,
  type SuiteStats,
  type SuiteTestChanges,
} from '@/api/types'
import {
  buildHistoryPoints,
  compareWithBaseline,
  median,
  hasRejectedTime,
  isHigherBetter,
  DEFAULT_CHANGE_WINDOW,
  type BaselineComparison,
  type HistoryMetric,
} from './testHistory'

export const REGRESSION_THRESHOLD_OPTIONS = [5, 10, 20, 30] as const
export const DEFAULT_REGRESSION_THRESHOLD = 10

// A change in a small test does not matter much: a block with little gas runs
// in a few milliseconds, and its MGas/s is noisy. A change counts only when the
// test uses at least this much gas, since the step or before it. In millions of
// gas; 0 keeps all tests. Gas, not time, sets the size: a time limit hides the
// large tests of a fast client.
export const MIN_GAS_OPTIONS_M = [0, 10, 50, 100, 200] as const
export const DEFAULT_MIN_GAS_M = 50

// A change in a fast block does not matter much either: 59ms to 11ms is still
// fast, and timer noise is large at that scale. A change counts only when the
// test takes at least this long, since the step or before it. In
// milliseconds; 0 keeps all tests. Invalid-payload tests use the time to
// reject.
export const MIN_DURATION_OPTIONS_MS = [0, 25, 50, 100, 250, 500] as const
export const DEFAULT_MIN_DURATION_MS = 100

// A change must also be this many times the normal spread of the baseline
// runs. This keeps noisy tests out of the list.
export const NOISE_FACTOR = 3

// With fewer than SHORT_BASELINE_RUNS runs before the step, the normal spread
// is a weak estimate, and a small change can pass the noise limit by chance.
// Such a change must be SHORT_BASELINE_FACTOR times the threshold. A step soon
// after another step has a short baseline, so a large change still counts.
export const SHORT_BASELINE_RUNS = 5
export const SHORT_BASELINE_FACTOR = 2

export type ChangeDirection = 'regression' | 'improvement'

export interface TestChange {
  testName: string
  client: string
  // MGas/s, or the reject time for the invalid-payload tests.
  metric: HistoryMetric
  step: ChangeStep
  baseline: BaselineStats
  direction: ChangeDirection
}

export interface ChangeOptions {
  // The smallest change that counts, in percent.
  threshold: number
  // The smallest test that counts, in millions of gas.
  minGasM: number
  // The shortest test that counts, in milliseconds.
  minDurationMs: number
}

// Returns the smallest change, in percent, that counts for this baseline.
export function significantChange(baseline: BaselineComparison, threshold: number): number {
  const minChange = baseline.runs < SHORT_BASELINE_RUNS ? threshold * SHORT_BASELINE_FACTOR : threshold
  return Math.max(minChange, NOISE_FACTOR * baseline.noise)
}

// Returns the direction of the change, or undefined when the change is
// smaller than the threshold or than the normal spread of the baseline.
export function classifyChange(
  baseline: BaselineComparison,
  metric: HistoryMetric,
  threshold: number,
): ChangeDirection | undefined {
  if (Math.abs(baseline.change) < significantChange(baseline, threshold)) return undefined

  const better = isHigherBetter(metric) ? baseline.change > 0 : baseline.change < 0
  return better ? 'improvement' : 'regression'
}

// Median of the positive values of the given runs, or 0.
function medianPositive(runIds: string[], valueByRun: Map<string, number>): number {
  const values = runIds.map((id) => valueByRun.get(id) ?? 0).filter((v) => v > 0)
  return values.length > 0 ? median(values) : 0
}

// Finds the latest step of each client and test in its last `window` runs. The
// API does this for an indexed suite (/index/suites/{hash}/changes); this
// builds the same shape from a stats.json.
export function buildSuiteChanges(
  stats: SuiteStats,
  stepFilter: IndexStepType[],
  window: number = DEFAULT_CHANGE_WINDOW,
): SuiteChanges {
  const tests: SuiteTestChanges[] = []
  // The index has no cleanup stats, so the API ignores the cleanup step. Do
  // the same, so both give the same series.
  const steps = stepFilter.filter((s) => s !== 'cleanup')
  if (steps.length === 0) steps.push('test')

  for (const [name, { durations }] of Object.entries(stats)) {
    const metric = hasRejectedTime(durations) ? 'rejected' : 'mgas'
    const pointsByClient = buildHistoryPoints(durations, metric, steps)
    // An invalid payload reports no gas, so its step time is 0. Its duration
    // is the time to reject.
    const durationByRun = new Map<string, number>()
    const durationPoints = metric === 'rejected' ? pointsByClient : buildHistoryPoints(durations, 'duration', steps)
    for (const points of durationPoints.values()) {
      for (const p of points) durationByRun.set(p.runId, p.value)
    }
    const gasByRun = new Map<string, number>()
    for (const d of durations) gasByRun.set(d.id, getRunDurationAggregatedStats(d, steps).gasUsed)
    const clients: ClientChange[] = []

    for (const [client, allPoints] of pointsByClient) {
      // Like the API, which reads only the last runs of each client.
      const points = allPoints.slice(-window)
      const comparison = compareWithBaseline(points, window)
      if (!comparison) continue

      const { stepIndex, stepRuns, stepMedian, ...baseline } = comparison
      const since = points.slice(stepIndex)
      const before = points.slice(stepIndex - baseline.runs, stepIndex)

      clients.push({
        client,
        step: {
          run_id: since[0].runId,
          run_start: since[0].runStart,
          runs: stepRuns,
          median: stepMedian,
          duration_ns: medianPositive(
            since.map((p) => p.runId),
            durationByRun,
          ),
          gas_used: medianPositive(
            since.map((p) => p.runId),
            gasByRun,
          ),
        },
        baseline: {
          ...baseline,
          last_run_id: before[before.length - 1].runId,
          last_run_start: before[before.length - 1].runStart,
          duration_ns: medianPositive(
            before.map((p) => p.runId),
            durationByRun,
          ),
          gas_used: medianPositive(
            before.map((p) => p.runId),
            gasByRun,
          ),
        },
      })
    }

    if (clients.length > 0) tests.push({ name, metric, clients })
  }

  return { tests }
}

// Returns the significant changes of the suite. The largest changes come first.
export function findTestChanges(
  suiteChanges: SuiteChanges,
  { threshold, minGasM, minDurationMs }: ChangeOptions,
): TestChange[] {
  const changes: TestChange[] = []
  const minGas = minGasM * 1_000_000
  const minDurationNs = minDurationMs * 1_000_000

  for (const test of suiteChanges.tests) {
    for (const { client, step, baseline } of test.clients) {
      const direction = classifyChange(baseline, test.metric, threshold)
      if (!direction) continue
      // An invalid payload reports no gas, so the gas limit does not apply.
      if (test.metric !== 'rejected' && Math.max(step.gas_used, baseline.gas_used) < minGas) continue
      if (Math.max(step.duration_ns, baseline.duration_ns) < minDurationNs) continue

      changes.push({ testName: test.name, client, metric: test.metric, step, baseline, direction })
    }
  }

  return changes.sort(
    (a, b) =>
      severity(b) - severity(a) || a.testName.localeCompare(b.testName) || a.client.localeCompare(b.client),
  )
}

// A drop from 100 to 50 MGas/s is -50%, but the same slowdown in time is
// +100%. Compare on the ratio, so both metrics rank the same way.
function severity(change: TestChange): number {
  const ratio = 1 + change.baseline.change / 100
  return Math.abs(Math.log(ratio))
}

// The most runs before a step that the search for a run on the same host
// reads. Each one is a config.json download.
export const MAX_SAME_HOST_LOOKBACK = 20

// Returns the runs of a client before the step run, newest first, and leaves
// out the last run before the step, which the caller already has. These are
// the runs to search for a run on the same host as the step run.
export function sameHostCandidates(
  durations: { id: string; client: string; run_start: number }[],
  client: string,
  stepRunId: string,
  lastRunId: string,
  max: number = MAX_SAME_HOST_LOOKBACK,
): string[] {
  const runs = durations
    .filter((d) => d.client === client)
    .sort((a, b) => a.run_start - b.run_start || a.id.localeCompare(b.id))
  const stepIndex = runs.findIndex((d) => d.id === stepRunId)
  if (stepIndex < 0) return []

  return runs
    .slice(0, stepIndex)
    .reverse()
    .map((d) => d.id)
    .filter((id) => id !== lastRunId)
    .slice(0, max)
}
