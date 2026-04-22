import type { RunResult, AggregatedStats, TestEntry, ResourceTotals } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'

export interface AveragedResult {
  result: RunResult
  // Per-test variance on MGas/s for error-bar and CV% display.
  variance: Record<string, { mgasStddev: number; mgasMean: number; mgasMin: number; mgasMax: number }>
}

/**
 * averageResults takes N RunResult objects and produces one synthetic
 * RunResult where each test's aggregated stats are the average (or
 * median) across the N runs. Tests that don't appear in all N runs
 * are still included — their average is taken over however many runs
 * contained them.
 */
export function averageResults(
  results: RunResult[],
  stepFilter: StepTypeOption[],
  mode: 'avg' | 'median',
): AveragedResult {
  if (results.length === 0) {
    return { result: { tests: {} }, variance: {} }
  }

  if (results.length === 1) {
    return { result: results[0], variance: {} }
  }

  // Gather per-test samples.
  const testSamples = new Map<
    string,
    {
      gasUsedTotal: number[]
      gasUsedTimeTotal: number[]
      timeTotal: number[]
      success: number[]
      fail: number[]
      msgCount: number[]
      cpuUsec: number[]
      memoryDeltaBytes: number[]
      memoryBytes: number[]
      diskReadBytes: number[]
      diskWriteBytes: number[]
      diskReadIops: number[]
      diskWriteIops: number[]
    }
  >()

  for (const result of results) {
    for (const [name, entry] of Object.entries(result.tests)) {
      const stats = getAggregatedStats(entry, stepFilter)
      if (!stats) continue

      let samples = testSamples.get(name)
      if (!samples) {
        samples = {
          gasUsedTotal: [],
          gasUsedTimeTotal: [],
          timeTotal: [],
          success: [],
          fail: [],
          msgCount: [],
          cpuUsec: [],
          memoryDeltaBytes: [],
          memoryBytes: [],
          diskReadBytes: [],
          diskWriteBytes: [],
          diskReadIops: [],
          diskWriteIops: [],
        }
        testSamples.set(name, samples)
      }

      samples.gasUsedTotal.push(stats.gas_used_total)
      samples.gasUsedTimeTotal.push(stats.gas_used_time_total)
      samples.timeTotal.push(stats.time_total)
      samples.success.push(stats.success)
      samples.fail.push(stats.fail)
      samples.msgCount.push(stats.msg_count)

      // getAggregatedStats doesn't include resource_totals in its
      // return value, so read it directly from the step entries.
      for (const stepKey of stepFilter) {
        const step = entry.steps?.[stepKey]
        const r = step?.aggregated?.resource_totals
        if (r) {
          samples.cpuUsec.push(r.cpu_usec ?? 0)
          samples.memoryDeltaBytes.push(r.memory_delta_bytes ?? 0)
          samples.memoryBytes.push(r.memory_bytes ?? 0)
          samples.diskReadBytes.push(r.disk_read_bytes ?? 0)
          samples.diskWriteBytes.push(r.disk_write_bytes ?? 0)
          samples.diskReadIops.push(r.disk_read_iops ?? 0)
          samples.diskWriteIops.push(r.disk_write_iops ?? 0)
          break // one step's resource data per test is enough
        }
      }
    }
  }

  // Compute aggregates.
  const agg = mode === 'avg' ? mean : median
  const syntheticTests: Record<string, TestEntry> = {}
  const variance: AveragedResult['variance'] = {}

  for (const [name, samples] of testSamples) {
    const gasUsedTotal = agg(samples.gasUsedTotal)
    const gasUsedTimeTotal = agg(samples.gasUsedTimeTotal)

    // Build resource_totals if we have samples for it.
    let resource_totals: ResourceTotals | undefined
    if (samples.cpuUsec.length > 0) {
      resource_totals = {
        cpu_usec: agg(samples.cpuUsec),
        memory_delta_bytes: agg(samples.memoryDeltaBytes),
        memory_bytes: agg(samples.memoryBytes),
        disk_read_bytes: agg(samples.diskReadBytes),
        disk_write_bytes: agg(samples.diskWriteBytes),
        disk_read_iops: agg(samples.diskReadIops),
        disk_write_iops: agg(samples.diskWriteIops),
      }
    }

    const aggregated: AggregatedStats = {
      gas_used_total: gasUsedTotal,
      gas_used_time_total: gasUsedTimeTotal,
      time_total: agg(samples.timeTotal),
      success: Math.round(agg(samples.success)),
      fail: Math.round(agg(samples.fail)),
      msg_count: Math.round(agg(samples.msgCount)),
      method_stats: { times: {}, mgas_s: {} },
      resource_totals,
    }

    // Build the TestEntry with the active step populated.
    // The comparison components read via getAggregatedStats which
    // iterates the step keys from the filter, so we put the averaged
    // data under the first step in the filter (usually "test").
    const stepKey = stepFilter[0] ?? 'test'
    syntheticTests[name] = {
      dir: '',
      steps: {
        [stepKey]: { aggregated },
      },
    }

    // Compute MGas/s variance for error bars.
    const mgasValues = samples.gasUsedTimeTotal.map((t, i) =>
      t > 0 ? (samples.gasUsedTotal[i] * 1000) / t : 0,
    )
    const mgasAvg = mean(mgasValues)
    const mgasStddev = stddev(mgasValues, mgasAvg)
    variance[name] = {
      mgasStddev,
      mgasMean: mgasAvg,
      mgasMin: Math.min(...mgasValues),
      mgasMax: Math.max(...mgasValues),
    }
  }

  return {
    result: { tests: syntheticTests },
    variance,
  }
}

// ── Stats helpers ────────────────────────────────────────────────

function mean(values: number[]): number {
  if (values.length === 0) return 0
  return values.reduce((a, b) => a + b, 0) / values.length
}

function median(values: number[]): number {
  if (values.length === 0) return 0
  const sorted = [...values].sort((a, b) => a - b)
  const mid = Math.floor(sorted.length / 2)
  return sorted.length % 2 === 0 ? (sorted[mid - 1] + sorted[mid]) / 2 : sorted[mid]
}

function stddev(values: number[], avg: number): number {
  if (values.length < 2) return 0
  const sumSq = values.reduce((sum, v) => sum + (v - avg) ** 2, 0)
  return Math.sqrt(sumSq / (values.length - 1))
}
