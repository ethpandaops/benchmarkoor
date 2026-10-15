import { describe, expect, it } from 'vitest'
import type { AggregatedStats, RunResult } from '@/api/types'
import { averageResults } from './averageResults'

function result(test: Partial<AggregatedStats>): RunResult {
  return {
    tests: {
      invalid: {
        dir: '1',
        steps: {
          test: {
            aggregated: {
              time_total: 0,
              gas_used_total: 0,
              gas_used_time_total: 0,
              success: 1,
              fail: 0,
              msg_count: 1,
              method_stats: { times: {}, mgas_s: {} },
              ...test,
            },
          },
        },
      },
    },
  }
}

describe('averageResults with rejected payloads', () => {
  it('keeps the rejection and averages the time to reject', () => {
    const averaged = averageResults(
      [
        result({ rejected: 1, rejected_time_total: 40_000_000, time_total: 40_000_000 }),
        result({ rejected: 1, rejected_time_total: 60_000_000, time_total: 60_000_000 }),
      ],
      ['test'],
      'avg',
    )
    const stats = averaged.result.tests.invalid.steps?.test?.aggregated
    expect(stats?.rejected).toBe(1)
    expect(stats?.rejected_time_total).toBe(50_000_000)
  })

  it('averages the time to reject over the runs that rejected only', () => {
    const averaged = averageResults(
      [
        result({ rejected: 1, rejected_time_total: 40_000_000 }),
        // This run failed: the client did not reject the payload.
        result({ success: 0, fail: 1 }),
      ],
      ['test'],
      'avg',
    )
    const stats = averaged.result.tests.invalid.steps?.test?.aggregated
    expect(stats?.rejected).toBe(1)
    expect(stats?.rejected_time_total).toBe(40_000_000)
  })

  it('leaves a test without rejections unchanged', () => {
    const averaged = averageResults(
      [result({ gas_used_total: 10, gas_used_time_total: 5 })],
      ['test'],
      'avg',
    )
    const stats = averaged.result.tests.invalid.steps?.test?.aggregated
    expect(stats?.rejected).toBeUndefined()
    expect(stats?.rejected_time_total).toBeUndefined()
  })
})
