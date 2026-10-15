import { describe, expect, it } from 'vitest'
import type { AggregatedStats, SuiteTest } from '@/api/types'
import { fixtureGasByTest, isRejectedOnly, payloadTimeNs, speedupVsBaselinePct, testGasForFilter } from './compareMetrics'

function stats(fields: Partial<AggregatedStats>): AggregatedStats {
  return {
    time_total: 0,
    gas_used_total: 0,
    gas_used_time_total: 0,
    success: 1,
    fail: 0,
    msg_count: 1,
    method_stats: { times: {}, mgas_s: {} },
    ...fields,
  }
}

const valid = (gas: number, timeNs: number) => stats({ gas_used_total: gas, gas_used_time_total: timeNs })
const rejected = (timeNs: number) => stats({ rejected: 1, rejected_time_total: timeNs })

describe('isRejectedOnly', () => {
  it('is true for a test whose only payloads were rejected', () => {
    expect(isRejectedOnly(rejected(40_000_000))).toBe(true)
  })

  it('is false for a test with gas, even next to a rejection', () => {
    expect(isRejectedOnly(stats({ gas_used_total: 1, gas_used_time_total: 1, rejected: 1, rejected_time_total: 5 }))).toBe(false)
    expect(isRejectedOnly(valid(10_000_000, 1_000_000))).toBe(false)
    expect(isRejectedOnly(undefined)).toBe(false)
  })
})

describe('payloadTimeNs', () => {
  it('is the gas time of a valid test', () => {
    expect(payloadTimeNs(valid(10_000_000, 2_000_000))).toBe(2_000_000)
  })

  it('is the time to reject of a rejected test', () => {
    expect(payloadTimeNs(rejected(46_800_000))).toBe(46_800_000)
  })

  it('is undefined without a payload time', () => {
    expect(payloadTimeNs(stats({}))).toBeUndefined()
    expect(payloadTimeNs(undefined)).toBeUndefined()
  })
})

describe('speedupVsBaselinePct', () => {
  it('compares MGas/s for valid tests', () => {
    // 10M gas in 1ms (10000 MGas/s) vs 2ms (5000 MGas/s): 100% faster.
    expect(speedupVsBaselinePct(valid(10_000_000, 1_000_000), valid(10_000_000, 2_000_000))).toBeCloseTo(100)
  })

  it('compares the time to reject for rejected tests', () => {
    // Rejecting in 20ms against a 40ms baseline is 100% faster; 80ms is 50% slower.
    expect(speedupVsBaselinePct(rejected(20_000_000), rejected(40_000_000))).toBeCloseTo(100)
    expect(speedupVsBaselinePct(rejected(80_000_000), rejected(40_000_000))).toBeCloseTo(-50)
  })

  it('does not compare a rejected test with a valid one', () => {
    expect(speedupVsBaselinePct(rejected(20_000_000), valid(10_000_000, 1_000_000))).toBeUndefined()
    expect(speedupVsBaselinePct(valid(10_000_000, 1_000_000), rejected(20_000_000))).toBeUndefined()
  })
})

describe('testGasForFilter', () => {
  it('uses the gas a valid test reports', () => {
    expect(testGasForFilter(valid(60_000_000, 1_000_000), 1)).toBe(60_000_000)
  })

  it('uses the fixture gas for a rejected test', () => {
    expect(testGasForFilter(rejected(20_000_000), 59_997_000)).toBe(59_997_000)
    expect(testGasForFilter(rejected(20_000_000), undefined)).toBe(0)
  })
})

describe('fixtureGasByTest', () => {
  it('maps tests to the benchmark gas their fixture records', () => {
    const tests = [
      { name: 'invalid', eest: { info: { 'fixture-format': 'x', metadata: { benchmark_gas_used: 9_996_000 } } } },
      { name: 'valid', eest: { info: { 'fixture-format': 'x' } } },
    ] as SuiteTest[]
    expect(fixtureGasByTest(tests)).toEqual(new Map([['invalid', 9_996_000]]))
    expect(fixtureGasByTest(undefined).size).toBe(0)
  })
})
