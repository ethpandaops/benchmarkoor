import type { RunResult, AggregatedStats, TestEntry, ResourceTotals } from '@/api/types'
import { type StepTypeOption, ALL_STEP_TYPES, getAggregatedStats } from '@/pages/RunDetailPage'

export interface AveragedResult {
  result: RunResult
  // Per-test variance on MGas/s for error-bar and CV% display.
  variance: Record<string, { mgasStddev: number; mgasMean: number; mgasMin: number; mgasMax: number }>
}

// Per-step samples across runs. Statistics are gathered for every step
// (setup/test/cleanup) independently so the averaged result preserves each
// step's resource usage — the resource charts can then isolate setup vs test.
interface StepSamples {
  gasUsedTotal: number[]
  gasUsedTimeTotal: number[]
  timeTotal: number[]
  success: number[]
  fail: number[]
  msgCount: number[]
  // Resource samples are only collected for runs where the step reported them.
  cpuUsec: number[]
  memoryDeltaBytes: number[]
  memoryBytes: number[]
  diskReadBytes: number[]
  diskWriteBytes: number[]
  diskReadIops: number[]
  diskWriteIops: number[]
}

function emptyStepSamples(): StepSamples {
  return {
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
}

/**
 * averageResults takes N RunResult objects and produces one synthetic
 * RunResult where each test's aggregated stats are the average (or
 * median) across the N runs. Tests that don't appear in all N runs
 * are still included — their average is taken over however many runs
 * contained them.
 *
 * Each step (setup/test/cleanup) is averaged independently and written back to
 * its own step in the synthetic entry, so the table (which sums the active
 * `stepFilter`) and the resource charts (which can isolate a single step) both
 * read correct per-step data. For the default single-step filter this is
 * identical to averaging the summed stats.
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

  // Per-step samples per test, plus the MGas/s samples (summed over the active
  // step filter) used for the error bars — kept separate so variance still
  // reflects run-to-run variability of the filtered total, as before.
  const perStepSamples = new Map<string, Partial<Record<StepTypeOption, StepSamples>>>()
  const mgasSamples = new Map<string, { gasUsedTotal: number[]; gasUsedTimeTotal: number[] }>()

  for (const result of results) {
    for (const [name, entry] of Object.entries(result.tests)) {
      // Match the previous inclusion rule: a test is present only if it has data
      // for the active step filter in at least one run.
      const stats = getAggregatedStats(entry, stepFilter)
      if (!stats) continue

      let mgas = mgasSamples.get(name)
      if (!mgas) {
        mgas = { gasUsedTotal: [], gasUsedTimeTotal: [] }
        mgasSamples.set(name, mgas)
      }
      mgas.gasUsedTotal.push(stats.gas_used_total)
      mgas.gasUsedTimeTotal.push(stats.gas_used_time_total)

      let steps = perStepSamples.get(name)
      if (!steps) {
        steps = {}
        perStepSamples.set(name, steps)
      }

      for (const stepKey of ALL_STEP_TYPES) {
        const aggregated = entry.steps?.[stepKey]?.aggregated
        if (!aggregated) continue

        let samples = steps[stepKey]
        if (!samples) {
          samples = emptyStepSamples()
          steps[stepKey] = samples
        }

        samples.gasUsedTotal.push(aggregated.gas_used_total)
        samples.gasUsedTimeTotal.push(aggregated.gas_used_time_total)
        samples.timeTotal.push(aggregated.time_total)
        samples.success.push(aggregated.success)
        samples.fail.push(aggregated.fail)
        samples.msgCount.push(aggregated.msg_count)

        const r = aggregated.resource_totals
        if (r) {
          samples.cpuUsec.push(r.cpu_usec ?? 0)
          samples.memoryDeltaBytes.push(r.memory_delta_bytes ?? 0)
          samples.memoryBytes.push(r.memory_bytes ?? 0)
          samples.diskReadBytes.push(r.disk_read_bytes ?? 0)
          samples.diskWriteBytes.push(r.disk_write_bytes ?? 0)
          samples.diskReadIops.push(r.disk_read_iops ?? 0)
          samples.diskWriteIops.push(r.disk_write_iops ?? 0)
        }
      }
    }
  }

  // Compute aggregates.
  const agg = mode === 'avg' ? mean : median
  const syntheticTests: Record<string, TestEntry> = {}
  const variance: AveragedResult['variance'] = {}

  for (const [name, steps] of perStepSamples) {
    const syntheticSteps: TestEntry['steps'] = {}

    for (const stepKey of ALL_STEP_TYPES) {
      const samples = steps[stepKey]
      if (!samples) continue

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
        gas_used_total: agg(samples.gasUsedTotal),
        gas_used_time_total: agg(samples.gasUsedTimeTotal),
        time_total: agg(samples.timeTotal),
        success: Math.round(agg(samples.success)),
        fail: Math.round(agg(samples.fail)),
        msg_count: Math.round(agg(samples.msgCount)),
        method_stats: { times: {}, mgas_s: {} },
        resource_totals,
      }

      syntheticSteps[stepKey] = { aggregated }
    }

    syntheticTests[name] = { dir: '', steps: syntheticSteps }

    // Compute MGas/s variance for error bars over the active step filter.
    const mgas = mgasSamples.get(name)
    const gasUsedTotal = mgas?.gasUsedTotal ?? []
    const gasUsedTimeTotal = mgas?.gasUsedTimeTotal ?? []
    const mgasValues = gasUsedTimeTotal.map((t, i) => (t > 0 ? (gasUsedTotal[i] * 1000) / t : 0))
    const mgasAvg = mean(mgasValues)
    variance[name] = {
      mgasStddev: stddev(mgasValues, mgasAvg),
      mgasMean: mgasAvg,
      mgasMin: mgasValues.length > 0 ? Math.min(...mgasValues) : 0,
      mgasMax: mgasValues.length > 0 ? Math.max(...mgasValues) : 0,
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
