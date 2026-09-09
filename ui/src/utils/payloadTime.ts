import type { StepResult, TestEntry } from '@/api/types'

// Step types a test result can carry. Same union as StepTypeOption on the
// run detail page, declared here so this helper stays free of page imports.
export type PayloadStepType = 'setup' | 'test' | 'cleanup'

// Engine API method that submits a block. The version suffix changes with
// the fork, so the match is on the stable part of the name.
const PAYLOAD_METHOD = 'newPayload'

export interface PayloadTimes {
  /** Sum of the payload durations of the selected steps, in nanoseconds. */
  totalNs: number
  /** Duration of the slowest single payload, in nanoseconds. */
  maxNs: number
  /** Number of payload calls in the selected steps. */
  count: number
}

/**
 * Collect the engine_newPayload durations of a test.
 *
 * `gas_used_time_total` gives the sum directly, because the executor only
 * adds the calls that report gas. The per-payload maximum comes from the
 * method stats, which carry `max` when a step has more than one call and
 * only `last` when it has one.
 */
export function getPayloadTimes(entry: TestEntry, stepFilter: PayloadStepType[]): PayloadTimes {
  const empty: PayloadTimes = { totalNs: 0, maxNs: 0, count: 0 }
  if (!entry.steps) return empty

  const stepMap: Record<PayloadStepType, StepResult | undefined> = {
    setup: entry.steps.setup,
    test: entry.steps.test,
    cleanup: entry.steps.cleanup,
  }

  let totalNs = 0
  let maxNs = 0
  let count = 0

  for (const type of stepFilter) {
    const aggregated = stepMap[type]?.aggregated
    if (!aggregated) continue

    totalNs += aggregated.gas_used_time_total

    for (const [method, stats] of Object.entries(aggregated.method_stats?.times ?? {})) {
      if (!method.includes(PAYLOAD_METHOD)) continue
      count += stats.count
      maxNs = Math.max(maxNs, stats.max ?? stats.last)
    }
  }

  // Fall back to the total when a step reports gas time but no payload
  // method stats. One payload then covers the whole step time.
  if (count === 0) return { totalNs, maxNs: totalNs, count: totalNs > 0 ? 1 : 0 }

  return { totalNs, maxNs, count }
}
