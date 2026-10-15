import type { AggregatedStats, LiveTestStats, TestEntry } from '@/api/types'

// liveTestsToHeatmapEntries adapts the live per-test gas map into the
// Record<string, TestEntry> shape that the existing TestHeatmap consumes.
// Only the `test` step's aggregated gas/duration is populated — that's
// all the heatmap reads when DEFAULT_INDEX_STEP_FILTER is active. Failed
// tests carry zero gas (rendered as no-data tiles by the heatmap).
export function liveTestsToHeatmapEntries(tests: Record<string, LiveTestStats>): Record<string, TestEntry> {
  const out: Record<string, TestEntry> = {}

  for (const [name, t] of Object.entries(tests)) {
    out[name] = {
      dir: '',
      steps: {
        test: { aggregated: liveTestStatsToAggregated(t) },
      },
    }
  }

  return out
}

export function liveTestStatsToAggregated(t: LiveTestStats): AggregatedStats {
  const gasUsed = t.gas_used ?? 0
  const gasUsedDurationNs = t.gas_used_duration_ns ?? 0
  const rejectedDurationNs = t.rejected_duration_ns ?? 0

  return {
    time_total: gasUsedDurationNs + rejectedDurationNs,
    gas_used_total: gasUsed,
    gas_used_time_total: gasUsedDurationNs,
    // A rejected test shows as rejected, not as "no data".
    ...(rejectedDurationNs > 0 ? { rejected: 1, rejected_time_total: rejectedDurationNs } : {}),
    success: t.passed ? 1 : 0,
    fail: t.passed ? 0 : 1,
    msg_count: 0,
    method_stats: { times: {}, mgas_s: {} },
  }
}
