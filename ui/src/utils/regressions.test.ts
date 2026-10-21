import { describe, expect, it } from 'vitest'

import type { RunDuration, SuiteStats } from '@/api/types'
import {
  buildSuiteChanges,
  classifyChange,
  findTestChanges,
  NOISE_FACTOR,
  SHORT_BASELINE_FACTOR,
  SHORT_BASELINE_RUNS,
  sameHostCandidates,
  type ChangeOptions,
} from './regressions'
import { compareWithBaseline, type HistoryPoint } from './testHistory'

const ALL = { threshold: 10, minGasM: 0, minDurationMs: 0 }

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

// The same runs with gasUsed in the test step instead of 100M gas.
function withGas(durations: RunDuration[], gasUsed: number): RunDuration[] {
  return durations.map((d) => ({ ...d, gas_used: gasUsed, steps: { test: { ...d.steps!.test!, gas_used: gasUsed } } }))
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

  it('starts the baseline at a step soon before the latest step', () => {
    // Up from 100 to 130 for 4 runs, then down to 55. The 10 runs before the
    // latest step span both levels, and with both levels the spread hides
    // the step down.
    const values = [100, 104, 97, 102, 95, 101, 99, 103, 96, 100, 98, 102, 97, 101, 99, 100, 131, 128, 133, 129, 55, 60]
    const baseline = compareWithBaseline(points(values))!

    expect(baseline.stepIndex).toBe(20)
    expect(baseline.stepRuns).toBe(2)
    expect(baseline.runs).toBe(4)
    expect(baseline.median).toBe(130)
    expect(baseline.change).toBeCloseTo(((57.5 - 130) / 130) * 100)
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
  const steady = compareWithBaseline(points([100, 100, 100, 100, 100, 85]))!

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
    // The spread is about 14.8%, so a change must be about 44% to count.
    const noisy = compareWithBaseline(points([90, 100, 110, 90, 110, 70]))!
    expect(noisy.noise * NOISE_FACTOR).toBeGreaterThan(40)
    expect(classifyChange(noisy, 'mgas', 10)).toBeUndefined()

    const bigDrop = compareWithBaseline(points([90, 100, 110, 90, 110, 50]))!
    expect(classifyChange(bigDrop, 'mgas', 10)).toBe('regression')
  })

  it('needs a larger change with few runs before the step', () => {
    // 4 runs before the step, so the change must be at least 2 × 10%.
    const small = compareWithBaseline(points([100, 100, 100, 100, 85]))!
    expect(small.runs).toBe(SHORT_BASELINE_RUNS - 1)
    expect(SHORT_BASELINE_FACTOR * 10).toBe(20)
    expect(classifyChange(small, 'mgas', 10)).toBeUndefined()

    const large = compareWithBaseline(points([100, 100, 100, 100, 75]))!
    expect(classifyChange(large, 'mgas', 10)).toBe('regression')
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
    expect(geth.step.gas_used).toBe(150_000_000)
    expect(geth.baseline.gas_used).toBe(150_000_000)
  })

  it('leaves out a test where no client has a baseline', () => {
    expect(buildSuiteChanges({ t: { durations: runsAt('geth', [100, 100, 50]) } }, ['test']).tests).toEqual([])
  })
})

describe('findTestChanges', () => {
  it('finds the changes of each client and test, largest first', () => {
    const stats: SuiteStats = {
      'test_a': {
        durations: [...runsAt('geth', [100, 100, 100, 100, 100, 80]), ...runsAt('reth', [100, 100, 100, 100, 100, 100])],
      },
      'test_b': {
        durations: [...runsAt('geth', [100, 100, 100, 100, 100, 50]), ...runsAt('reth', [100, 100, 100, 100, 100, 130])],
      },
    }

    const changes = changesOf(stats, ALL)

    expect(changes.map((c) => [c.testName, c.client, c.direction])).toEqual([
      ['test_b', 'geth', 'regression'],
      ['test_b', 'reth', 'improvement'],
      ['test_a', 'geth', 'regression'],
    ])
    expect(changes[0].step.run_id).toBe('geth-6')
    expect(changes[0].baseline.last_run_id).toBe('geth-5')
    expect(changes[0].baseline.change).toBeCloseTo(-50)
  })

  it('ranks a slowdown the same for MGas/s and for time', () => {
    // Half the MGas/s and twice the reject time are the same slowdown.
    const stats: SuiteStats = {
      'mgas_test': { durations: runsAt('geth', [100, 100, 100, 100, 100, 50]) },
      'invalid_test': {
        durations: [1, 2, 3, 4, 5].map((i) => run('geth', i, 1_000, 1_000)).concat(run('geth', 6, 1_000, 1_900)),
      },
    }

    const changes = changesOf(stats, ALL)

    expect(changes.map((c) => [c.testName, c.metric])).toEqual([
      ['mgas_test', 'mgas'],
      ['invalid_test', 'rejected'],
    ])
    expect(changes[1].direction).toBe('regression')
  })

  it('skips a test with less gas than the minimum', () => {
    const small: SuiteStats = { 'test_a': { durations: withGas(runsAt('geth', [100, 100, 100, 100, 100, 50]), 10_000_000) } }
    const opts = { threshold: 10, minGasM: 50, minDurationMs: 0 }

    expect(changesOf(small, opts)).toEqual([])
    expect(changesOf(small, { ...opts, minGasM: 10 })).toHaveLength(1)
  })

  it('keeps a large test of a fast client', () => {
    // 100M gas at 1000 MGas/s takes 100ms, and 250ms at 400 MGas/s. A fast
    // block still counts when it has enough gas.
    const stats: SuiteStats = { 'test_a': { durations: runsAt('nethermind', [1000, 1000, 1000, 1000, 1000, 400]) } }

    const [change] = changesOf(stats, { threshold: 10, minGasM: 50, minDurationMs: 100 })

    expect(change.direction).toBe('regression')
    expect(change.step.gas_used).toBe(100_000_000)
    expect(change.step.duration_ns).toBe(250_000_000)
  })

  it('skips a test faster than the min. block time on both sides of the step', () => {
    // 100M gas at 2000 MGas/s takes 50ms. At 10000 MGas/s it takes 10ms.
    const fast: SuiteStats = { 'test_a': { durations: runsAt('geth', [2000, 2000, 2000, 2000, 2000, 10000]) } }
    const opts = { threshold: 10, minGasM: 50, minDurationMs: 100 }

    expect(changesOf(fast, opts)).toEqual([])
    expect(changesOf(fast, { ...opts, minDurationMs: 50 })).toHaveLength(1)
  })

  it('keeps a fast test that became slow', () => {
    // From 50ms before the step to 500ms since the step.
    const stats: SuiteStats = { 'test_a': { durations: runsAt('geth', [2000, 2000, 2000, 2000, 2000, 200]) } }

    const [change] = changesOf(stats, { threshold: 10, minGasM: 50, minDurationMs: 100 })

    expect(change.direction).toBe('regression')
    expect(change.step.duration_ns).toBe(500_000_000)
    expect(change.baseline.duration_ns).toBe(50_000_000)
  })

  it('keeps an invalid-payload test, which reports no gas', () => {
    // The time to reject goes from 600ms to 1.2s.
    const durations = withGas(
      [1, 2, 3, 4, 5].map((i) => run('geth', i, 0, 600_000_000)).concat(run('geth', 6, 0, 1_200_000_000)),
      0,
    )

    // The min. block time compares with the time to reject.
    const [change] = changesOf({ invalid_test: { durations } }, { threshold: 10, minGasM: 200, minDurationMs: 500 })

    expect(change.direction).toBe('regression')
    expect(change.step.gas_used).toBe(0)
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

describe('sameHostCandidates', () => {
  const durations = [
    { id: 'g1', client: 'geth', run_start: 1 },
    { id: 'r1', client: 'reth', run_start: 2 },
    { id: 'g3', client: 'geth', run_start: 3 },
    { id: 'g2', client: 'geth', run_start: 2 },
    { id: 'g4', client: 'geth', run_start: 4 },
    { id: 'g5', client: 'geth', run_start: 5 },
  ]

  it('lists the runs of the client before the step, newest first, without the last run', () => {
    expect(sameHostCandidates(durations, 'geth', 'g4', 'g3')).toEqual(['g2', 'g1'])
  })

  it('stops at the limit', () => {
    expect(sameHostCandidates(durations, 'geth', 'g5', 'g4', 2)).toEqual(['g3', 'g2'])
  })

  it('is empty when the step run is not in the history', () => {
    expect(sameHostCandidates(durations, 'geth', 'gone', 'g4')).toEqual([])
  })
})
