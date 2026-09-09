import { describe, it, expect } from 'vitest'
import type { AggregatedStats, MethodStats, TestEntry } from '@/api/types'
import { getPayloadTimes } from './payloadTime'

// step builds one aggregated step with a single payload method.
function step(gasTimeNs: number, payload?: Partial<MethodStats>): { aggregated: AggregatedStats } {
  return {
    aggregated: {
      time_total: gasTimeNs,
      gas_used_total: 1_000_000,
      gas_used_time_total: gasTimeNs,
      success: 1,
      fail: 0,
      msg_count: 1,
      method_stats: {
        times: {
          engine_forkchoiceUpdatedV4: { count: 1, last: 500 },
          ...(payload ? { engine_newPayloadV5: { count: 1, last: gasTimeNs, ...payload } as MethodStats } : {}),
        },
        mgas_s: {},
      },
    },
  }
}

describe('getPayloadTimes', () => {
  it('returns zeroes for a test without steps', () => {
    expect(getPayloadTimes({ dir: '' }, ['test'])).toEqual({ totalNs: 0, maxNs: 0, count: 0 })
  })

  it('reads a single payload from the last value', () => {
    const entry: TestEntry = { dir: '', steps: { test: step(4_000_000_000, {}) } }
    expect(getPayloadTimes(entry, ['test'])).toEqual({
      totalNs: 4_000_000_000,
      maxNs: 4_000_000_000,
      count: 1,
    })
  })

  it('takes the slowest call when a step has several payloads', () => {
    const entry: TestEntry = {
      dir: '',
      steps: { test: step(9_000_000_000, { count: 3, max: 5_000_000_000, last: 1_000_000_000 }) },
    }
    expect(getPayloadTimes(entry, ['test'])).toEqual({
      totalNs: 9_000_000_000,
      maxNs: 5_000_000_000,
      count: 3,
    })
  })

  it('takes the slowest call across the selected steps only', () => {
    const entry: TestEntry = {
      dir: '',
      steps: {
        setup: step(7_000_000_000, {}),
        test: step(2_000_000_000, {}),
      },
    }
    expect(getPayloadTimes(entry, ['test'])).toEqual({
      totalNs: 2_000_000_000,
      maxNs: 2_000_000_000,
      count: 1,
    })
    expect(getPayloadTimes(entry, ['setup', 'test'])).toEqual({
      totalNs: 9_000_000_000,
      maxNs: 7_000_000_000,
      count: 2,
    })
  })

  it('falls back to the step total when no payload method is present', () => {
    const entry: TestEntry = { dir: '', steps: { test: step(3_000_000_000) } }
    expect(getPayloadTimes(entry, ['test'])).toEqual({
      totalNs: 3_000_000_000,
      maxNs: 3_000_000_000,
      count: 1,
    })
  })
})
