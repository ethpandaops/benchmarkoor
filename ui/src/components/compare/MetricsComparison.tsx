import clsx from 'clsx'
import type { RunConfig, RunResult, AggregatedStats } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { Duration } from '@/components/shared/Duration'
import { formatDuration, formatNumber } from '@/utils/format'
import { formatDurationSeconds } from '@/utils/date'
import { type CompareRun, type LabelMode, RUN_SLOTS, formatRunLabel } from './constants'

interface MetricsComparisonProps {
  runs: CompareRun[]
  stepFilter: StepTypeOption[]
  baselineIdx: number
  onBaselineChange: (idx: number) => void
  labelMode: LabelMode
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

function DeltaIndicator({ value, formatValue, higherIsBetter = true }: { value: number; formatValue?: (v: number) => string; higherIsBetter?: boolean }) {
  if (value === 0) return <span className="text-xs/5 text-gray-400 dark:text-gray-500">-</span>
  const isPositive = value > 0
  const isGood = higherIsBetter ? isPositive : !isPositive
  const arrow = isPositive ? '\u25B2' : '\u25BC'
  const formatted = formatValue ? formatValue(Math.abs(value)) : Math.abs(value).toFixed(2)

  return (
    <span className={clsx('text-xs/5 font-medium', isGood ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400')}>
      {arrow} {formatted}
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
  formatDelta,
  extra,
  baselineIdx = 0,
  runLabels,
}: {
  label: string
  values: React.ReactNode[]
  clients: string[]
  deltas?: (number | undefined)[]
  percentValues?: (number | undefined)[]
  higherIsBetter?: boolean
  formatDelta?: (v: number) => string
  extra?: React.ReactNode[]
  baselineIdx?: number
  runLabels: string[]
}) {
  const hasDeltas = deltas?.some((d, i) => i !== baselineIdx && d !== undefined)

  return (
    <div className="rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
      <p className="mb-2 text-sm/6 font-medium text-gray-500 dark:text-gray-400">{label}</p>
      <table className="w-full divide-y divide-gray-100 dark:divide-gray-700/50">
        <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
          {values.map((val, i) => {
            const slot = RUN_SLOTS[i]
            const delta = deltas?.[i]
            const isBaseline = i === baselineIdx
            return (
              <tr key={slot.label}>
                <td className="w-5 py-0.5 align-middle">
                  <img src={`/img/clients/${clients[i]}.jpg`} alt={clients[i]} className="size-4 rounded-full object-cover" />
                </td>
                <td className={clsx('py-0.5 align-middle text-xs/5 font-semibold', slot.textClass, `dark:${slot.textDarkClass.replace('text-', 'text-')}`)}>
                  {runLabels[i]}
                </td>
                <td className="py-0.5 align-middle text-base/6 font-semibold text-gray-900 dark:text-gray-100">
                  {val}
                </td>
                {extra && (
                  <td className="py-0.5 pl-2 text-right align-middle">
                    {extra[i]}
                  </td>
                )}
                {hasDeltas && (
                  <td className="py-0.5 pl-2 text-right align-middle">
                    {!isBaseline && delta !== undefined && (
                      <span className="flex items-center justify-end gap-1">
                        <DeltaIndicator value={delta} formatValue={formatDelta} higherIsBetter={higherIsBetter} />
                        {percentValues && (
                          <PercentDelta a={percentValues[baselineIdx]} b={percentValues[i]} higherIsBetter={higherIsBetter} />
                        )}
                      </span>
                    )}
                  </td>
                )}
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

export function MetricsComparison({ runs, stepFilter, baselineIdx, onBaselineChange, labelMode }: MetricsComparisonProps) {
  const metrics = runs.map((r) => computeMetrics(r.config, r.result, stepFilter))
  const clients = runs.map((r) => r.config.instance.client)
  const runLabels = runs.map((r) => formatRunLabel(RUN_SLOTS[r.index], r, labelMode))
  const base = metrics[baselineIdx]

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-1.5 text-xs/5 text-gray-500 dark:text-gray-400">
        <span>Baseline:</span>
        <div className="flex gap-1">
          {runs.map((run, i) => {
            const slot = RUN_SLOTS[run.index]
            return (
              <button
                key={slot.label}
                onClick={() => onBaselineChange(i)}
                className={clsx(
                  'inline-flex items-center gap-1 rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                  baselineIdx === i
                    ? `${slot.badgeBgClass} ${slot.badgeTextClass} ring-1 ring-current`
                    : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
                )}
              >
                <img
                  src={`/img/clients/${run.config.instance.client}.jpg`}
                  alt={run.config.instance.client}
                  className="size-3.5 rounded-full object-cover"
                />
                {formatRunLabel(slot, run, labelMode)}
              </button>
            )
          })}
        </div>
      </div>
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <MetricCard
          label="Tests"
          clients={clients}
          baselineIdx={baselineIdx}
          runLabels={runLabels}
          values={metrics.map((m) => m.testCount)}
          extra={metrics.map((m) => (
            <span className="flex items-center justify-end gap-1.5">
              {m.failedTests > 0 && <span className="text-xs font-medium text-red-600 dark:text-red-400">{m.failedTests} Failed</span>}
              <span className="text-xs font-medium text-green-600 dark:text-green-400">{m.passedTests} Passed</span>
            </span>
          ))}
        />
        <MetricCard
          label="MGas/s"
          clients={clients}
          baselineIdx={baselineIdx}
          runLabels={runLabels}
          values={metrics.map((m) => m.mgasPerSec !== undefined ? m.mgasPerSec.toFixed(2) : '-')}
          deltas={metrics.map((m, i) => i === baselineIdx ? undefined : (m.mgasPerSec !== undefined && base.mgasPerSec !== undefined ? m.mgasPerSec - base.mgasPerSec : undefined))}
          percentValues={metrics.map((m) => m.mgasPerSec)}
          higherIsBetter
        />
        <MetricCard
          label="Total Gas"
          clients={clients}
          baselineIdx={baselineIdx}
          runLabels={runLabels}
          values={metrics.map((m) => formatGas(m.totalGasUsed))}
        />
        <MetricCard
          label="Test Duration"
          clients={clients}
          baselineIdx={baselineIdx}
          runLabels={runLabels}
          values={metrics.map((m) => <Duration nanoseconds={m.totalDuration} />)}
          deltas={metrics.map((m, i) => i === baselineIdx ? undefined : (m.totalDuration > 0 && base.totalDuration > 0 ? m.totalDuration - base.totalDuration : undefined))}
          percentValues={metrics.map((m) => m.totalDuration)}
          higherIsBetter={false}
          formatDelta={(v) => formatDuration(v)}
        />
        <MetricCard
          label="Total Runtime"
          clients={clients}
          baselineIdx={baselineIdx}
          runLabels={runLabels}
          values={metrics.map((m) => m.totalRuntime !== undefined ? formatDurationSeconds(m.totalRuntime) : '-')}
          deltas={metrics.map((m, i) => i === baselineIdx ? undefined : (m.totalRuntime !== undefined && base.totalRuntime !== undefined ? m.totalRuntime - base.totalRuntime : undefined))}
          percentValues={metrics.map((m) => m.totalRuntime)}
          higherIsBetter={false}
          formatDelta={(v) => formatDurationSeconds(v)}
        />
        <MetricCard
          label="Calls"
          clients={clients}
          baselineIdx={baselineIdx}
          runLabels={runLabels}
          values={metrics.map((m) => formatNumber(m.totalMsgCount))}
        />
      </div>
    </div>
  )
}
