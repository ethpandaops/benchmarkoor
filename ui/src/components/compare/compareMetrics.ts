// Per-run summary metrics of the compare pages, over the tests that
// pass the page filter. Shared by the metric cards of the run compare
// page and the ranking table of the group compare page.

import type { AggregatedStats, RunConfig, RunResult, SuiteTest } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'

export interface ComputedMetrics {
  testCount: number
  passedTests: number
  failedTests: number
  totalDuration: number
  totalGasUsed: number
  totalGasUsedTime: number
  mgasPerSec: number | undefined
  totalMsgCount: number
  totalRuntime: number | undefined
}

export function computeMetrics(config: RunConfig, result: RunResult | null, stepFilter: StepTypeOption[], testNameFilter?: (name: string) => boolean): ComputedMetrics {
  const filteredTests = result
    ? Object.entries(result.tests).filter(([name]) => !testNameFilter || testNameFilter(name))
    : []

  const aggregatedStats = filteredTests
    .map(([, t]) => getAggregatedStats(t, stepFilter))
    .filter((s): s is AggregatedStats => s !== undefined)

  const testCount = testNameFilter
    ? filteredTests.length
    : (config.test_counts?.total ?? (result ? Object.keys(result.tests).length : 0))
  const passedTests = testNameFilter
    ? aggregatedStats.filter((s) => s.fail === 0).length
    : (config.test_counts?.passed ?? aggregatedStats.filter((s) => s.fail === 0).length)
  const failedTests = testCount - passedTests
  const totalDuration = aggregatedStats.reduce((sum, s) => sum + s.time_total, 0)
  const totalGasUsed = aggregatedStats.reduce((sum, s) => sum + s.gas_used_total, 0)
  const totalGasUsedTime = aggregatedStats.reduce((sum, s) => sum + s.gas_used_time_total, 0)
  const mgasPerSec = totalGasUsedTime > 0 ? (totalGasUsed * 1000) / totalGasUsedTime : undefined
  const totalMsgCount = aggregatedStats.reduce((sum, s) => sum + s.msg_count, 0)
  const totalRuntime = config.timestamp_end && config.timestamp_end > 0
    ? config.timestamp_end - config.timestamp
    : undefined

  return { testCount, passedTests, failedTests, totalDuration, totalGasUsed, totalGasUsedTime, mgasPerSec, totalMsgCount, totalRuntime }
}

/**
 * True when a test's only payloads are ones the client must reject: it has a
 * time to reject but no MGas/s (the client can stop part way through the
 * block, and the header gasUsed can be the corrupted field).
 */
export function isRejectedOnly(stats: AggregatedStats | undefined): boolean {
  return !!stats && stats.gas_used_time_total <= 0 && (stats.rejected ?? 0) > 0
}

/**
 * The payload time the compare pages show for a test: the time of the
 * payloads that report gas, or for a rejected-only test the time the client
 * took to reject its payloads. undefined when the test has neither.
 */
export function payloadTimeNs(stats: AggregatedStats | undefined): number | undefined {
  if (!stats) return undefined
  if (stats.gas_used_time_total > 0) return stats.gas_used_time_total
  if (isRejectedOnly(stats) && (stats.rejected_time_total ?? 0) > 0) return stats.rejected_time_total
  return undefined
}

/**
 * How much faster a run is than the baseline on one test, in percent
 * (+50 = 50% faster). Tests that report gas compare MGas/s. Rejected-only
 * tests compare the time to reject: for the same block, the run that takes
 * less time is faster, and baseline_time / time is the MGas/s ratio the block
 * would have. undefined when the two runs cannot be compared.
 */
export function speedupVsBaselinePct(
  stats: AggregatedStats | undefined,
  base: AggregatedStats | undefined,
): number | undefined {
  if (!stats || !base) return undefined
  if (isRejectedOnly(stats) || isRejectedOnly(base)) {
    if (!isRejectedOnly(stats) || !isRejectedOnly(base)) return undefined
    const time = stats.rejected_time_total ?? 0
    const baseTime = base.rejected_time_total ?? 0
    if (time <= 0 || baseTime <= 0) return undefined
    return (baseTime / time - 1) * 100
  }
  if (stats.gas_used_time_total <= 0 || stats.gas_used_total <= 0) return undefined
  if (base.gas_used_time_total <= 0 || base.gas_used_total <= 0) return undefined
  const mgas = (stats.gas_used_total * 1000) / stats.gas_used_time_total
  const baseMgas = (base.gas_used_total * 1000) / base.gas_used_time_total
  return ((mgas - baseMgas) / baseMgas) * 100
}

/**
 * The gas of a test for the gas filter. A rejected test reports no gas of its
 * own (the client rejects its block); its fixture records the gas the
 * benchmark uses (`_info.metadata.benchmark_gas_used`).
 */
export function testGasForFilter(stats: AggregatedStats, fixtureGas: number | undefined): number {
  if (stats.gas_used_total > 0 || !isRejectedOnly(stats)) return stats.gas_used_total
  return fixtureGas ?? 0
}

/** Fixture gas per test name, from the suite's EEST info. */
export function fixtureGasByTest(suiteTests: SuiteTest[] | undefined): Map<string, number> {
  const map = new Map<string, number>()
  for (const t of suiteTests ?? []) {
    const gas = t.eest?.info?.metadata?.benchmark_gas_used
    if (gas !== undefined) map.set(t.name, gas)
  }
  return map
}

export function formatGas(gas: number): string {
  if (gas >= 1_000_000_000) return `${(gas / 1_000_000_000).toFixed(2)} GGas`
  return `${(gas / 1_000_000).toFixed(2)} MGas`
}
