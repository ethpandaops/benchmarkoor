import clsx from 'clsx'
import type { RunConfig, RunResult, AggregatedStats } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { Duration } from '@/components/shared/Duration'
import { formatNumber } from '@/utils/format'
import { formatDurationSeconds } from '@/utils/date'

interface MetricsComparisonProps {
  configA: RunConfig
  configB: RunConfig
  resultA: RunResult | null
  resultB: RunResult | null
  stepFilter: StepTypeOption[]
}

interface ComputedMetrics {
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

function computeMetrics(config: RunConfig, result: RunResult | null, stepFilter: StepTypeOption[]): ComputedMetrics {
  const aggregatedStats = result
    ? Object.values(result.tests).map((t) => getAggregatedStats(t, stepFilter)).filter((s): s is AggregatedStats => s !== undefined)
    : []

  const testCount = config.test_counts?.total ?? (result ? Object.keys(result.tests).length : 0)
  const passedTests = config.test_counts?.passed ?? aggregatedStats.filter((s) => s.fail === 0).length
  const failedTests = config.test_counts ? (config.test_counts.total - config.test_counts.passed) : aggregatedStats.filter((s) => s.fail > 0).length
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

function formatGas(gas: number): string {
  if (gas >= 1_000_000_000) return `${(gas / 1_000_000_000).toFixed(2)} GGas`
  return `${(gas / 1_000_000).toFixed(2)} MGas`
}

function DeltaIndicator({ value, suffix = '', higherIsBetter = true }: { value: number; suffix?: string; higherIsBetter?: boolean }) {
  if (value === 0) return <span className="text-xs/5 text-gray-400 dark:text-gray-500">-</span>
  const isPositive = value > 0
  const isGood = higherIsBetter ? isPositive : !isPositive
  const arrow = isPositive ? '\u25B2' : '\u25BC'
  const absValue = Math.abs(value)

  return (
    <span className={clsx('text-xs/5 font-medium', isGood ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400')}>
      {arrow} {absValue.toFixed(2)}{suffix}
    </span>
  )
}

function PercentDelta({ a, b, higherIsBetter = true }: { a: number | undefined; b: number | undefined; higherIsBetter?: boolean }) {
  if (a === undefined || b === undefined || a === 0) return null
  const pct = ((b - a) / a) * 100
  if (Math.abs(pct) < 0.01) return null

  const isGood = higherIsBetter ? pct > 0 : pct < 0
  return (
    <span className={clsx('text-xs/5', isGood ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400')}>
      ({pct > 0 ? '+' : ''}{pct.toFixed(1)}%)
    </span>
  )
}

function MetricCard({
  label,
  valueA,
  valueB,
  delta,
  percentA,
  percentB,
  higherIsBetter = true,
}: {
  label: string
  valueA: React.ReactNode
  valueB: React.ReactNode
  delta?: number
  percentA?: number
  percentB?: number
  higherIsBetter?: boolean
}) {
  return (
    <div className="rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
      <p className="text-sm/6 font-medium text-gray-500 dark:text-gray-400">{label}</p>
      <div className="mt-2 grid grid-cols-3 gap-2">
        <div>
          <p className="text-xs/5 font-medium text-blue-600 dark:text-blue-400">A</p>
          <p className="text-lg/7 font-semibold text-gray-900 dark:text-gray-100">{valueA}</p>
        </div>
        <div>
          <p className="text-xs/5 font-medium text-amber-600 dark:text-amber-400">B</p>
          <p className="text-lg/7 font-semibold text-gray-900 dark:text-gray-100">{valueB}</p>
        </div>
        <div className="flex flex-col items-end justify-center gap-0.5">
          {delta !== undefined && <DeltaIndicator value={delta} higherIsBetter={higherIsBetter} />}
          {percentA !== undefined && percentB !== undefined && (
            <PercentDelta a={percentA} b={percentB} higherIsBetter={higherIsBetter} />
          )}
        </div>
      </div>
    </div>
  )
}

export function MetricsComparison({ configA, configB, resultA, resultB, stepFilter }: MetricsComparisonProps) {
  const a = computeMetrics(configA, resultA, stepFilter)
  const b = computeMetrics(configB, resultB, stepFilter)

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <MetricCard
        label="Tests"
        valueA={
          <span className="flex items-center gap-1">
            {a.testCount}
            <span className="text-xs text-green-600 dark:text-green-400">{a.passedTests}P</span>
            {a.failedTests > 0 && <span className="text-xs text-red-600 dark:text-red-400">{a.failedTests}F</span>}
          </span>
        }
        valueB={
          <span className="flex items-center gap-1">
            {b.testCount}
            <span className="text-xs text-green-600 dark:text-green-400">{b.passedTests}P</span>
            {b.failedTests > 0 && <span className="text-xs text-red-600 dark:text-red-400">{b.failedTests}F</span>}
          </span>
        }
      />
      <MetricCard
        label="MGas/s"
        valueA={a.mgasPerSec !== undefined ? a.mgasPerSec.toFixed(2) : '-'}
        valueB={b.mgasPerSec !== undefined ? b.mgasPerSec.toFixed(2) : '-'}
        delta={a.mgasPerSec !== undefined && b.mgasPerSec !== undefined ? b.mgasPerSec - a.mgasPerSec : undefined}
        percentA={a.mgasPerSec}
        percentB={b.mgasPerSec}
        higherIsBetter
      />
      <MetricCard
        label="Total Gas"
        valueA={formatGas(a.totalGasUsed)}
        valueB={formatGas(b.totalGasUsed)}
      />
      <MetricCard
        label="Test Duration"
        valueA={<Duration nanoseconds={a.totalDuration} />}
        valueB={<Duration nanoseconds={b.totalDuration} />}
        delta={a.totalDuration > 0 && b.totalDuration > 0 ? b.totalDuration - a.totalDuration : undefined}
        percentA={a.totalDuration}
        percentB={b.totalDuration}
        higherIsBetter={false}
      />
      <MetricCard
        label="Total Runtime"
        valueA={a.totalRuntime !== undefined ? formatDurationSeconds(a.totalRuntime) : '-'}
        valueB={b.totalRuntime !== undefined ? formatDurationSeconds(b.totalRuntime) : '-'}
        delta={a.totalRuntime !== undefined && b.totalRuntime !== undefined ? b.totalRuntime - a.totalRuntime : undefined}
        percentA={a.totalRuntime}
        percentB={b.totalRuntime}
        higherIsBetter={false}
      />
      <MetricCard
        label="Calls"
        valueA={formatNumber(a.totalMsgCount)}
        valueB={formatNumber(b.totalMsgCount)}
      />
    </div>
  )
}
