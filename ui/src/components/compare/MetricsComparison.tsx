import clsx from 'clsx'
import type { RunConfig, RunResult, AggregatedStats } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { Duration } from '@/components/shared/Duration'
import { formatNumber } from '@/utils/format'
import { formatDurationSeconds } from '@/utils/date'
import { type CompareRun, RUN_SLOTS } from './constants'

interface MetricsComparisonProps {
  runs: CompareRun[]
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
  values,
  clients,
  deltas,
  percentValues,
  higherIsBetter = true,
}: {
  label: string
  values: React.ReactNode[]
  clients: string[]
  deltas?: (number | undefined)[]
  percentValues?: (number | undefined)[]
  higherIsBetter?: boolean
}) {
  return (
    <div className="rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
      <p className="mb-2 text-sm/6 font-medium text-gray-500 dark:text-gray-400">{label}</p>
      <div className="flex flex-col gap-1">
        {values.map((val, i) => {
          const slot = RUN_SLOTS[i]
          const delta = deltas?.[i]
          const isBaseline = i === 0
          return (
            <div key={slot.label} className="flex items-center gap-2">
              <img src={`/img/clients/${clients[i]}.jpg`} alt={clients[i]} className="size-4 rounded-full object-cover" />
              <span className={clsx('w-3 text-xs/5 font-semibold', slot.textClass, `dark:${slot.textDarkClass.replace('text-', 'text-')}`)}>{slot.label}</span>
              <span className="text-base/6 font-semibold text-gray-900 dark:text-gray-100">{val}</span>
              {!isBaseline && delta !== undefined && (
                <span className="flex items-center gap-1">
                  <DeltaIndicator value={delta} higherIsBetter={higherIsBetter} />
                  {percentValues && (
                    <PercentDelta a={percentValues[0]} b={percentValues[i]} higherIsBetter={higherIsBetter} />
                  )}
                </span>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

export function MetricsComparison({ runs, stepFilter }: MetricsComparisonProps) {
  const metrics = runs.map((r) => computeMetrics(r.config, r.result, stepFilter))
  const clients = runs.map((r) => r.config.instance.client)

  return (
    <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
      <MetricCard
        label="Tests"
        clients={clients}
        values={metrics.map((m) => (
          <span className="flex items-center gap-1">
            {m.testCount}
            <span className="text-xs text-green-600 dark:text-green-400">{m.passedTests}P</span>
            {m.failedTests > 0 && <span className="text-xs text-red-600 dark:text-red-400">{m.failedTests}F</span>}
          </span>
        ))}
      />
      <MetricCard
        label="MGas/s"
        clients={clients}
        values={metrics.map((m) => m.mgasPerSec !== undefined ? m.mgasPerSec.toFixed(2) : '-')}
        deltas={metrics.map((m, i) => i === 0 ? undefined : (m.mgasPerSec !== undefined && metrics[0].mgasPerSec !== undefined ? m.mgasPerSec - metrics[0].mgasPerSec : undefined))}
        percentValues={metrics.map((m) => m.mgasPerSec)}
        higherIsBetter
      />
      <MetricCard
        label="Total Gas"
        clients={clients}
        values={metrics.map((m) => formatGas(m.totalGasUsed))}
      />
      <MetricCard
        label="Test Duration"
        clients={clients}
        values={metrics.map((m) => <Duration nanoseconds={m.totalDuration} />)}
        deltas={metrics.map((m, i) => i === 0 ? undefined : (m.totalDuration > 0 && metrics[0].totalDuration > 0 ? m.totalDuration - metrics[0].totalDuration : undefined))}
        percentValues={metrics.map((m) => m.totalDuration)}
        higherIsBetter={false}
      />
      <MetricCard
        label="Total Runtime"
        clients={clients}
        values={metrics.map((m) => m.totalRuntime !== undefined ? formatDurationSeconds(m.totalRuntime) : '-')}
        deltas={metrics.map((m, i) => i === 0 ? undefined : (m.totalRuntime !== undefined && metrics[0].totalRuntime !== undefined ? m.totalRuntime - metrics[0].totalRuntime : undefined))}
        percentValues={metrics.map((m) => m.totalRuntime)}
        higherIsBetter={false}
      />
      <MetricCard
        label="Calls"
        clients={clients}
        values={metrics.map((m) => formatNumber(m.totalMsgCount))}
      />
    </div>
  )
}
