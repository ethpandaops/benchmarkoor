import { describe, expect, it } from 'vitest'

import type { RunDuration, SuiteStats } from '@/api/types'
import { buildSuiteChanges, classifyChange, findTestChanges, NOISE_FACTOR, type ChangeOptions } from './regressions'
import { compareWithBaseline, type HistoryPoint } from './testHistory'

const ALL = { threshold: 10, minDurationMs: 0 }

function changesOf(stats: SuiteStats, opts: ChangeOptions) {
  return findTestChanges(buildSuiteChanges(stats, ['test']), opts)
}

// One run of 100M gas in the test step that takes timeNs.
function run(client: string, runStart: number, timeNs: number, rejectedNs?: number): RunDuration {
  return {
    id: `${client}-${runStart}`,
    client,
    gas_used: 100_000_000,
    time_ns: timeNs,
    rejected_time_ns: rejectedNs,
    run_start: runStart,
    steps: { test: { gas_used: 100_000_000, time_ns: timeNs, rejected_time_ns: rejectedNs } },
  }
}

// Runs with the given MGas/s values, oldest first.
function runsAt(client: string, mgas: number[]): RunDuration[] {
  return mgas.map((v, i) => run(client, i + 1, 100_000_000_000 / v))
}

function points(values: number[]): HistoryPoint[] {
  return values.map((value, i) => ({ runId: `run-${i}`, client: 'geth', runStart: i, value }))
}

const repeat = (v: number, n: number) => Array.from({ length: n }, () => v)

// The same cases as TestCompareWithBaseline in pkg/api, so both
// implementations of the rule give the same result.
describe('compareWithBaseline', () => {
  it('measures the change and the normal spread of the baseline', () => {
    const baseline = compareWithBaseline(points([90, 100, 110, 100, 80]))!

    expect(baseline.stepIndex).toBe(4)
    expect(baseline.stepRuns).toBe(1)
    expect(baseline.stepMedian).toBe(80)
    expect(baseline.median).toBe(100)
    expect(baseline.runs).toBe(4)
    expect(baseline.change).toBeCloseTo(-20)
    // MAD of [90, 100, 110, 100] is 5, so the spread is 5 × 1.4826 = 7.4%.
    expect(baseline.noise).toBeCloseTo(7.413)
  })

  it('needs at least three earlier runs', () => {
    expect(compareWithBaseline(points([100, 100, 50]))).toBeUndefined()
    expect(compareWithBaseline(points([100, 100, 100, 50]))).toBeDefined()
  })

  it('keeps finding an old step', () => {
    const baseline = compareWithBaseline(points([...repeat(100, 20), 70, 71, 69, 70, 72, 70, 68, 70, 71, 70]))!

    expect(baseline.stepIndex).toBe(20)
    expect(baseline.stepRuns).toBe(10)
    expect(baseline.stepMedian).toBe(70)
    expect(baseline.median).toBe(100)
    expect(baseline.runs).toBe(10)
    expect(baseline.change).toBeCloseTo(-30)
  })

  it('compares a step after a step with the level before it', () => {
    const baseline = compareWithBaseline(points([...repeat(100, 10), ...repeat(150, 15), ...repeat(120, 5)]))!

    expect(baseline.stepIndex).toBe(25)
    expect(baseline.median).toBe(150)
    expect(baseline.change).toBeCloseTo(-20)
  })

  it('sees a fixed regression as an improvement against the slow level', () => {
    const baseline = compareWithBaseline(points([...repeat(100, 15), ...repeat(70, 10), ...repeat(100, 5)]))!

    expect(baseline.stepIndex).toBe(25)
    expect(baseline.median).toBe(70)
    expect(baseline.change).toBeGreaterThan(0)
  })

  it('compares the latest run with the runs before it when there is no step', () => {
    const baseline = compareWithBaseline(points([100, 103, 98, 101, 97, 102, 99, 104, 100, 96, 101, 99]))!

    expect(baseline.stepRuns).toBe(1)
    expect(baseline.runs).toBe(10)
    expect(Math.abs(baseline.change)).toBeLessThan(5)
  })

  it('looks only at the last runs of the window', () => {
    // The slow runs are older than the last 30, so the old step is not found.
    const values = [...repeat(10, 5), ...repeat(100, 30)]

    expect(compareWithBaseline(points(values))!.stepRuns).toBe(1)
    expect(compareWithBaseline(points(values), 50)!.stepIndex).toBe(5)
  })
})

describe('classifyChange', () => {
  const steady = compareWithBaseline(points([100, 100, 100, 100, 85]))!

  it('marks a drop in MGas/s as a regression', () => {
    expect(classifyChange(steady, 'mgas', 10)).toBe('regression')
  })

  it('marks a drop in time as an improvement', () => {
    expect(classifyChange(steady, 'rejected', 10)).toBe('improvement')
  })

  it('ignores a change below the threshold', () => {
    expect(classifyChange(steady, 'mgas', 20)).toBeUndefined()
  })

  it('ignores a change inside the normal spread of the baseline', () => {
    // The spread is about 7.4%, so a change must be about 22% to count.
    const noisy = compareWithBaseline(points([90, 100, 110, 100, 80]))!
    expect(noisy.noise * NOISE_FACTOR).toBeGreaterThan(20)
    expect(classifyChange(noisy, 'mgas', 10)).toBeUndefined()

    const bigDrop = compareWithBaseline(points([90, 100, 110, 100, 70]))!
    expect(classifyChange(bigDrop, 'mgas', 10)).toBe('regression')
  })
})

describe('buildSuiteChanges', () => {
  // The same case as TestBuildSuiteChanges in pkg/api, so the API and the
  // stats.json path give the same result.
  it('sums the selected steps', () => {
    const durations = [1, 2, 3, 4, 5].map((i) => ({
      ...run('geth', i, i === 5 ? 2e9 : 1e9),
      steps: {
        setup: { gas_used: 50_000_000, time_ns: 1e9 },
        test: { gas_used: 100_000_000, time_ns: i === 5 ? 2e9 : 1e9 },
      },
    }))

    const [test] = buildSuiteChanges({ mgas_test: { durations } }, ['setup', 'test']).tests
    const [geth] = test.clients

    expect(test.metric).toBe('mgas')
    expect(geth.step.median).toBeCloseTo(50)
    expect(geth.baseline.median).toBeCloseTo(75)
    expect(geth.step.duration_ns).toBe(3e9)
    expect(geth.baseline.duration_ns).toBe(2e9)
  })

  it('leaves out a test where no client has a baseline', () => {
    expect(buildSuiteChanges({ t: { durations: runsAt('geth', [100, 100, 50]) } }, ['test']).tests).toEqual([])
  })
})

describe('findTestChanges', () => {
  it('finds the changes of each client and test, largest first', () => {
    const stats: SuiteStats = {
      'test_a': { durations: [...runsAt('geth', [100, 100, 100, 100, 80]), ...runsAt('reth', [100, 100, 100, 100, 100])] },
      'test_b': { durations: [...runsAt('geth', [100, 100, 100, 100, 50]), ...runsAt('reth', [100, 100, 100, 100, 130])] },
    }

    const changes = changesOf(stats, ALL)

    expect(changes.map((c) => [c.testName, c.client, c.direction])).toEqual([
      ['test_b', 'geth', 'regression'],
      ['test_b', 'reth', 'improvement'],
      ['test_a', 'geth', 'regression'],
    ])
    expect(changes[0].step.run_id).toBe('geth-5')
    expect(changes[0].baseline.change).toBeCloseTo(-50)
  })

  it('ranks a slowdown the same for MGas/s and for time', () => {
    // Half the MGas/s and twice the reject time are the same slowdown.
    const stats: SuiteStats = {
      'mgas_test': { durations: runsAt('geth', [100, 100, 100, 100, 50]) },
      'invalid_test': {
        durations: [1, 2, 3, 4].map((i) => run('geth', i, 1_000, 1_000)).concat(run('geth', 5, 1_000, 1_900)),
      },
    }

    const changes = changesOf(stats, ALL)

    expect(changes.map((c) => [c.testName, c.metric])).toEqual([
      ['mgas_test', 'mgas'],
      ['invalid_test', 'rejected'],
    ])
    expect(changes[1].direction).toBe('regression')
  })

  it('skips a test faster than the minimum duration', () => {
    // 100M gas at 1000 MGas/s takes 100ms. At 500 MGas/s it takes 200ms.
    const fast: SuiteStats = { 'test_a': { durations: runsAt('geth', [1000, 1000, 1000, 1000, 500]) } }
    const opts = { threshold: 10, minDurationMs: 250 }

    expect(changesOf(fast, opts)).toEqual([])
    expect(changesOf(fast, { ...opts, minDurationMs: 200 })).toHaveLength(1)
  })

  it('keeps a fast test that became slow', () => {
    // From 100ms in the baseline to 1s in the latest run.
    const stats: SuiteStats = { 'test_a': { durations: runsAt('geth', [1000, 1000, 1000, 1000, 100]) } }

    const [change] = changesOf(stats, { threshold: 10, minDurationMs: 500 })

    expect(change.direction).toBe('regression')
    expect(change.step.duration_ns).toBe(1_000_000_000)
    expect(change.baseline.duration_ns).toBe(100_000_000)
  })

  it('compares an invalid-payload test with the min. block time on its time to reject', () => {
    // No gas, so no step time. The time to reject goes from 600ms to 1.2s.
    const durations = [1, 2, 3, 4]
      .map((i) => run('geth', i, 0, 600_000_000))
      .concat(run('geth', 5, 0, 1_200_000_000))

    const [change] = changesOf({ invalid_test: { durations } }, { threshold: 10, minDurationMs: 500 })

    expect(change.direction).toBe('regression')
    expect(change.step.duration_ns).toBe(1_200_000_000)
    expect(change.baseline.duration_ns).toBe(600_000_000)
  })

  it('ignores the cleanup step, as the API does', () => {
    // A slow cleanup step must not change the test series.
    const durations = runsAt('geth', [100, 100, 100, 100, 50]).map((d) => ({
      ...d,
      steps: { ...d.steps, cleanup: { gas_used: 100_000_000, time_ns: 10e9 } },
    }))

    const [geth] = buildSuiteChanges({ t: { durations } }, ['test', 'cleanup']).tests[0].clients
    const [onlyCleanup] = buildSuiteChanges({ t: { durations } }, ['cleanup']).tests[0].clients

    expect(geth.baseline.change).toBeCloseTo(-50)
    expect(onlyCleanup).toEqual(geth)
  })

  it('skips a client with too few runs', () => {
    const stats: SuiteStats = { 'test_a': { durations: runsAt('geth', [100, 100, 50]) } }

    expect(changesOf(stats, ALL)).toEqual([])
  })
})
