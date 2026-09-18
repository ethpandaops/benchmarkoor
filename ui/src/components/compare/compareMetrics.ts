// Per-run summary metrics of the compare pages, over the tests that
// pass the page filter. Shared by the metric cards of the run compare
// page and the ranking table of the group compare page.

import type { AggregatedStats, RunConfig, RunResult } from '@/api/types'
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

export function formatGas(gas: number): string {
  if (gas >= 1_000_000_000) return `${(gas / 1_000_000_000).toFixed(2)} GGas`
  return `${(gas / 1_000_000).toFixed(2)} MGas`
}
