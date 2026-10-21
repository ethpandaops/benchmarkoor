import type {
  BaselineStats,
  ChangeStep,
  ClientChange,
  IndexStepType,
  SuiteChanges,
  SuiteStats,
  SuiteTestChanges,
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

// A change in a fast test does not matter: 30ms to 50ms is still fast. A change
// counts only when the test takes at least this long, since the step or before
// it. In milliseconds; 0 keeps all tests.
export const MIN_DURATION_OPTIONS_MS = [0, 100, 250, 500, 1000] as const
export const DEFAULT_MIN_DURATION_MS = 500

// A change must also be this many times the normal spread of the baseline
// runs. This keeps noisy tests out of the list.
export const NOISE_FACTOR = 3

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
  // The shortest test that counts, in milliseconds.
  minDurationMs: number
}

// Returns the smallest change, in percent, that counts for this baseline.
export function significantChange(baseline: BaselineComparison, threshold: number): number {
  return Math.max(threshold, NOISE_FACTOR * baseline.noise)
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

// Median of the positive durations of the given runs, or 0.
function medianDuration(runIds: string[], durationByRun: Map<string, number>): number {
  const durations = runIds.map((id) => durationByRun.get(id) ?? 0).filter((d) => d > 0)
  return durations.length > 0 ? median(durations) : 0
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
    // An invalid payload reports no gas, so its step time is 0. Its time to
    // reject is the time that the min. block time is compared with.
    const durationByRun = new Map<string, number>()
    const durationPoints = metric === 'rejected' ? pointsByClient : buildHistoryPoints(durations, 'duration', steps)
    for (const points of durationPoints.values()) {
      for (const p of points) durationByRun.set(p.runId, p.value)
    }
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
          duration_ns: medianDuration(
            since.map((p) => p.runId),
            durationByRun,
          ),
        },
        baseline: {
          ...baseline,
          duration_ns: medianDuration(
            before.map((p) => p.runId),
            durationByRun,
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
  { threshold, minDurationMs }: ChangeOptions,
): TestChange[] {
  const changes: TestChange[] = []
  const minDurationNs = minDurationMs * 1_000_000

  for (const test of suiteChanges.tests) {
    for (const { client, step, baseline } of test.clients) {
      const direction = classifyChange(baseline, test.metric, threshold)
      if (!direction) continue
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
