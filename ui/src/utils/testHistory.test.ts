import { describe, expect, it } from 'vitest'

import type { TestHistoryDuration } from '@/api/types'
import {
  buildHistoryPoints,
  findRunGaps,
  historyMetricValue,
  median,
  previousRuns,
  sortHistoryRuns,
  summarizeClient,
  type HistoryPoint,
} from './testHistory'

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
  it('compares the latest level with the median of the runs before it', () => {
    const summary = summarizeClient('geth', [point(1, 100), point(2, 110), point(3, 90), point(4, 80)])!

    expect(summary.count).toBe(4)
    expect(summary.latest.value).toBe(80)
    expect(summary.min).toBe(80)
    expect(summary.max).toBe(110)
    expect(summary.baseline?.change).toBeCloseTo(-20)
    expect(summary.since?.value).toBe(80)
  })

  it('compares a step after an older step with the level before it', () => {
    const old = Array.from({ length: 20 }, (_, i) => point(i, 1000))
    const recent = Array.from({ length: 10 }, (_, i) => point(100 + i, 100))
    const summary = summarizeClient('geth', [...old, ...recent, point(200, 110)])!

    expect(summary.baseline?.change).toBeCloseTo(10)
    expect(summary.baseline?.runs).toBe(10)
  })

  it('has no change with too few earlier runs', () => {
    expect(summarizeClient('geth', [point(1, 100), point(2, 50)])!.baseline).toBeUndefined()
    expect(summarizeClient('geth', [])).toBeUndefined()
  })
})

describe('findRunGaps', () => {
  const hour = 3600
  const day = 24 * hour

  it('breaks the line at a gap of days between runs every few hours', () => {
    // One run, a gap of 5 days, then a run every 7 hours.
    expect([...findRunGaps([0, 5 * day, 5 * day + 7 * hour, 5 * day + 14 * hour])]).toEqual([1])
  })

  it('keeps a steady series in one line', () => {
    expect(findRunGaps([0, day, 2 * day, 3 * day, 4 * day]).size).toBe(0)
  })

  it('ignores a short gap between frequent runs', () => {
    // Runs every 30 minutes, with one 4 hour gap: below the 6 hour minimum.
    const starts = [0, 1800, 3600, 3600 + 4 * hour, 5400 + 4 * hour]
    expect(findRunGaps(starts).size).toBe(0)
  })

  it('needs at least two intervals', () => {
    expect(findRunGaps([0, 10 * day]).size).toBe(0)
  })
})

describe('sortHistoryRuns', () => {
  // The test step takes timeNs for 100M gas, so 1s is 100 MGas/s.
  const run = (id: string, client: string, runStart: number, timeNs: number) =>
    duration({ id, client, run_start: runStart, steps: { test: { gas_used: 100_000_000, time_ns: timeNs } } })
  const runs = [run('a', 'reth', 3, 2e9), run('b', 'geth', 2, 1e9), run('c', 'besu', 1, 4e9)]
  const ids = (rows: TestHistoryDuration[]) => rows.map((d) => d.id)

  it('sorts by time, client and MGas/s', () => {
    expect(ids(sortHistoryRuns(runs, 'time', 'asc', ['test']))).toEqual(['c', 'b', 'a'])
    expect(ids(sortHistoryRuns(runs, 'client', 'asc', ['test']))).toEqual(['c', 'b', 'a'])
    expect(ids(sortHistoryRuns(runs, 'mgas', 'desc', ['test']))).toEqual(['b', 'a', 'c'])
    expect(ids(sortHistoryRuns(runs, 'duration', 'asc', ['test']))).toEqual(['b', 'a', 'c'])
  })

  it('puts a run with no value last in both directions', () => {
    // No time in the test step, so no MGas/s.
    const rows = [...runs, run('d', 'geth', 4, 0)]
    expect(ids(sortHistoryRuns(rows, 'mgas', 'asc', ['test']))[3]).toBe('d')
    expect(ids(sortHistoryRuns(rows, 'mgas', 'desc', ['test']))[3]).toBe('d')
  })

  it('keeps the newest run first in a tie', () => {
    const rows = [run('old', 'geth', 1, 1e9), run('new', 'geth', 2, 1e9)]
    expect(ids(sortHistoryRuns(rows, 'mgas', 'asc', ['test']))).toEqual(['new', 'old'])
  })
})

describe('previousRuns', () => {
  it('maps each run to the last run of the same client before it', () => {
    const rows = [
      duration({ id: 'g3', client: 'geth', run_start: 3 }),
      duration({ id: 'r2', client: 'reth', run_start: 2 }),
      duration({ id: 'g1', client: 'geth', run_start: 1 }),
      duration({ id: 'g2', client: 'geth', run_start: 2 }),
    ]
    const previous = previousRuns(rows)

    expect(previous.get('g3')?.id).toBe('g2')
    expect(previous.get('g2')?.id).toBe('g1')
    expect(previous.has('g1')).toBe(false)
    expect(previous.has('r2')).toBe(false)
  })
})
