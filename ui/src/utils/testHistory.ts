import { type IndexStepType, type TestHistoryDuration, getRunDurationAggregatedStats } from '@/api/types'
import { formatDuration } from '@/utils/format'

export type HistoryMetric = 'mgas' | 'duration' | 'rejected'

export const HISTORY_METRIC_LABELS: Record<HistoryMetric, string> = {
  mgas: 'MGas/s',
  duration: 'Duration',
  rejected: 'Reject time',
}

// The summary compares the latest run with the median of up to this many
// earlier runs of the same client.
export const BASELINE_RUNS = 10
// Below this many earlier runs the median is too noisy to compare with.
export const MIN_BASELINE_RUNS = 3

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
  // Change of the latest value against the median of the earlier runs, in
  // percent. Undefined when the client has too few earlier runs.
  change?: number
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

// Summarizes the points of one client. The points must be oldest first.
export function summarizeClient(client: string, points: HistoryPoint[]): ClientHistorySummary | undefined {
  if (points.length === 0) return undefined

  const values = points.map((p) => p.value)
  const latest = points[points.length - 1]
  const baseline = values.slice(-1 - BASELINE_RUNS, -1)
  const baselineMedian = median(baseline)

  return {
    client,
    count: points.length,
    latest,
    median: median(values),
    min: Math.min(...values),
    max: Math.max(...values),
    change:
      baseline.length >= MIN_BASELINE_RUNS && baselineMedian > 0
        ? ((latest.value - baselineMedian) / baselineMedian) * 100
        : undefined,
  }
}

// Formats a metric value for display.
export function formatHistoryValue(value: number, metric: HistoryMetric): string {
  return metric === 'mgas' ? `${value.toFixed(2)} MGas/s` : formatDuration(Math.round(value))
}
