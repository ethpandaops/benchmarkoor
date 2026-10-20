import { describe, expect, it } from 'vitest'

import type { TestHistoryDuration } from '@/api/types'
import { buildHistoryPoints, historyMetricValue, median, summarizeClient, type HistoryPoint } from './testHistory'

function duration(overrides: Partial<TestHistoryDuration>): TestHistoryDuration {
  return {
    id: 'run',
    client: 'geth',
    gas_used: 0,
    time_ns: 0,
    run_start: 0,
    steps: {
      setup: { gas_used: 50_000_000, time_ns: 500_000_000 },
      test: { gas_used: 100_000_000, time_ns: 1_000_000_000 },
    },
    ...overrides,
  }
}

function point(runStart: number, value: number): HistoryPoint {
  return { runId: `run-${runStart}`, client: 'geth', runStart, value }
}

describe('historyMetricValue', () => {
  it('computes MGas/s from the selected steps', () => {
    expect(historyMetricValue(duration({}), 'mgas', ['test'])).toBe(100)
    expect(historyMetricValue(duration({}), 'mgas', ['setup', 'test'])).toBe(100)
  })

  it('returns the time of the selected steps for the duration', () => {
    expect(historyMetricValue(duration({}), 'duration', ['setup', 'test'])).toBe(1_500_000_000)
  })

  it('has no MGas/s for a step without gas', () => {
    const d = duration({ steps: { test: { gas_used: 0, time_ns: 1_000 } } })
    expect(historyMetricValue(d, 'mgas', ['test'])).toBeUndefined()
    expect(historyMetricValue(d, 'duration', ['test'])).toBe(1_000)
  })

  it('sums the reject time of the selected steps', () => {
    const d = duration({
      rejected_time_ns: 60,
      steps: {
        setup: { gas_used: 0, time_ns: 0, rejected_time_ns: 13 },
        test: { gas_used: 0, time_ns: 0, rejected_time_ns: 47 },
      },
    })
    expect(historyMetricValue(d, 'rejected', ['test'])).toBe(47)
    expect(historyMetricValue(d, 'rejected', ['setup', 'test'])).toBe(60)
    expect(historyMetricValue(duration({}), 'rejected', ['test'])).toBeUndefined()
  })
})

describe('buildHistoryPoints', () => {
  it('groups by client, sorts oldest first and drops runs without a value', () => {
    const points = buildHistoryPoints(
      [
        duration({ id: 'b', client: 'reth', run_start: 200 }),
        duration({ id: 'a', client: 'geth', run_start: 300 }),
        duration({ id: 'c', client: 'geth', run_start: 100 }),
        duration({ id: 'd', client: 'geth', run_start: 50, steps: { test: { gas_used: 0, time_ns: 0 } } }),
      ],
      'mgas',
      ['test'],
    )

    expect([...points.keys()]).toEqual(['geth', 'reth'])
    expect(points.get('geth')!.map((p) => p.runId)).toEqual(['c', 'a'])
  })
})

describe('median', () => {
  it('handles odd and even lengths', () => {
    expect(median([3, 1, 2])).toBe(2)
    expect(median([4, 1, 3, 2])).toBe(2.5)
    expect(median([])).toBeNaN()
  })
})

describe('summarizeClient', () => {
  it('compares the latest run with the median of the earlier runs', () => {
    const summary = summarizeClient('geth', [point(1, 100), point(2, 110), point(3, 90), point(4, 80)])!

    expect(summary.count).toBe(4)
    expect(summary.latest.value).toBe(80)
    expect(summary.min).toBe(80)
    expect(summary.max).toBe(110)
    expect(summary.change).toBeCloseTo(-20)
  })

  it('uses only the most recent earlier runs as the baseline', () => {
    const old = Array.from({ length: 20 }, (_, i) => point(i, 1000))
    const recent = Array.from({ length: 10 }, (_, i) => point(100 + i, 100))
    const summary = summarizeClient('geth', [...old, ...recent, point(200, 110)])!

    expect(summary.change).toBeCloseTo(10)
  })

  it('has no change with too few earlier runs', () => {
    expect(summarizeClient('geth', [point(1, 100), point(2, 50)])!.change).toBeUndefined()
    expect(summarizeClient('geth', [])).toBeUndefined()
  })
})
