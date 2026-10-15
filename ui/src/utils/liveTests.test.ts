import { describe, expect, it } from 'vitest'
import { liveTestStatsToAggregated, liveTestsToHeatmapEntries } from './liveTests'

describe('liveTestStatsToAggregated', () => {
  it('keeps the gas and time of a valid test', () => {
    const agg = liveTestStatsToAggregated({ passed: true, gas_used: 60_000_000, gas_used_duration_ns: 50_000_000 })
    expect(agg.gas_used_total).toBe(60_000_000)
    expect(agg.time_total).toBe(50_000_000)
    expect(agg.rejected).toBeUndefined()
  })

  it('marks a rejected test with its time to reject', () => {
    const agg = liveTestStatsToAggregated({ passed: true, rejected_duration_ns: 61_000_000 })
    expect(agg.rejected).toBe(1)
    expect(agg.rejected_time_total).toBe(61_000_000)
    expect(agg.time_total).toBe(61_000_000)
    expect(agg.gas_used_total).toBe(0)
    expect(agg.success).toBe(1)
  })
})

describe('liveTestsToHeatmapEntries', () => {
  it('puts the stats on the test step', () => {
    const entries = liveTestsToHeatmapEntries({ a: { passed: false } })
    expect(entries.a.steps?.test?.aggregated?.fail).toBe(1)
  })
})
