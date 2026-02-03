import { useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import clsx from 'clsx'
import { type IndexEntry, type IndexStepType, getIndexAggregatedStats, ALL_INDEX_STEP_TYPES } from '@/api/types'
import { formatTimestamp } from '@/utils/date'
import { ClientBadge } from '@/components/shared/ClientBadge'

// Check if run completed successfully (no status = completed for backward compat)
function isRunCompleted(run: IndexEntry): boolean {
  return !run.status || run.status === 'completed'
}

const MAX_RUNS_PER_CLIENT = 30

// 5-level discrete color scale (green to red for duration, reversed for MGas/s)
const COLORS = [
  '#22c55e', // green - best
  '#84cc16', // lime
  '#eab308', // yellow
  '#f97316', // orange
  '#ef4444', // red - worst
]

function getColorByNormalizedValue(value: number, min: number, max: number, higherIsBetter: boolean): string {
  if (max === min) return COLORS[2] // middle color if all same
  let normalized = (value - min) / (max - min)
  if (higherIsBetter) normalized = 1 - normalized // reverse for MGas/s (higher is better)
  const level = Math.min(4, Math.floor(normalized * 5))
  return COLORS[level]
}

function formatDurationMinSec(nanoseconds: number): string {
  const seconds = nanoseconds / 1_000_000_000
  if (seconds < 60) return `${seconds.toFixed(1)}s`
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = Math.floor(seconds % 60)
  return `${minutes}m ${remainingSeconds}s`
}

function formatDurationCompact(nanoseconds: number): string {
  const seconds = nanoseconds / 1_000_000_000
  if (seconds < 60) return `${seconds.toFixed(0)}s`
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = Math.floor(seconds % 60)
  return `${minutes}m${remainingSeconds}s`
}

function calculateMGasPerSec(gasUsed: number, gasUsedDuration: number): number | undefined {
  if (gasUsedDuration <= 0 || gasUsed <= 0) return undefined
  return (gasUsed * 1000) / gasUsedDuration
}

function calculatePercentile(sortedValues: number[], percentile: number): number {
  if (sortedValues.length === 0) return 0
  const index = (percentile / 100) * (sortedValues.length - 1)
  const lower = Math.floor(index)
  const upper = Math.ceil(index)
  if (lower === upper) return sortedValues[lower]
  return sortedValues[lower] + (sortedValues[upper] - sortedValues[lower]) * (index - lower)
}

interface ClientStats {
  min?: number
  max?: number
  mean?: number
  p95?: number
  p99?: number
  last?: number
}

export type ColorNormalization = 'suite' | 'client'
export type MetricMode = 'duration' | 'mgas'

interface RunsHeatmapProps {
  runs: IndexEntry[]
  isDark: boolean
  colorNormalization?: ColorNormalization
  onColorNormalizationChange?: (mode: ColorNormalization) => void
  metricMode?: MetricMode
  onMetricModeChange?: (mode: MetricMode) => void
  stepFilter?: IndexStepType[]
}

interface TooltipData {
  run: IndexEntry
  x: number
  y: number
}

export function RunsHeatmap({
  runs,
  isDark,
  colorNormalization = 'suite',
  onColorNormalizationChange,
  metricMode: controlledMetricMode,
  onMetricModeChange,
  stepFilter = ALL_INDEX_STEP_TYPES,
}: RunsHeatmapProps) {
  const navigate = useNavigate()
  const [tooltip, setTooltip] = useState<TooltipData | null>(null)
  const [internalMetricMode, setInternalMetricMode] = useState<MetricMode>('mgas')

  const metricMode = controlledMetricMode ?? internalMetricMode
  const setMetricMode = (mode: MetricMode) => {
    if (onMetricModeChange) {
      onMetricModeChange(mode)
    } else {
      setInternalMetricMode(mode)
    }
  }

  const {
    clientRuns,
    minDuration,
    maxDuration,
    minMgas,
    maxMgas,
    clientDurationMinMax,
    clientMgasMinMax,
    clientDurationStats,
    clientMgasStats,
    clients,
  } = useMemo(() => {
    // Group runs by client
    const grouped: Record<string, IndexEntry[]> = {}
    for (const run of runs) {
      const client = run.instance.client
      if (!grouped[client]) grouped[client] = []
      grouped[client].push(run)
    }

    // Sort each client's runs by timestamp (oldest first) and take last N
    const clientRuns: Record<string, IndexEntry[]> = {}
    const clientDurationMinMax: Record<string, { min: number; max: number }> = {}
    const clientMgasMinMax: Record<string, { min: number; max: number }> = {}
    const clientDurationStats: Record<string, ClientStats> = {}
    const clientMgasStats: Record<string, ClientStats> = {}

    for (const [client, clientRunsAll] of Object.entries(grouped)) {
      const sorted = [...clientRunsAll].sort((a, b) => b.timestamp - a.timestamp)
      clientRuns[client] = sorted.slice(0, MAX_RUNS_PER_CLIENT)

      // Calculate duration stats
      const durations = clientRuns[client].map((r) => getIndexAggregatedStats(r, stepFilter).duration)
      const sortedDurations = [...durations].sort((a, b) => a - b)
      const durationSum = durations.reduce((acc, d) => acc + d, 0)

      clientDurationMinMax[client] = {
        min: sortedDurations[0],
        max: sortedDurations[sortedDurations.length - 1],
      }

      clientDurationStats[client] = {
        min: sortedDurations[0],
        max: sortedDurations[sortedDurations.length - 1],
        mean: durationSum / durations.length,
        p95: calculatePercentile(sortedDurations, 95),
        p99: calculatePercentile(sortedDurations, 99),
        last: durations[0],
      }

      // Calculate MGas/s stats
      const mgasValues = clientRuns[client]
        .map((r) => {
          const stats = getIndexAggregatedStats(r, stepFilter)
          return calculateMGasPerSec(stats.gasUsed, stats.gasUsedDuration)
        })
        .filter((v): v is number => v !== undefined)

      if (mgasValues.length > 0) {
        const sortedMgas = [...mgasValues].sort((a, b) => a - b)
        const mgasSum = mgasValues.reduce((acc, v) => acc + v, 0)

        clientMgasMinMax[client] = {
          min: sortedMgas[0],
          max: sortedMgas[sortedMgas.length - 1],
        }

        clientMgasStats[client] = {
          min: sortedMgas[0],
          max: sortedMgas[sortedMgas.length - 1],
          mean: mgasSum / mgasValues.length,
          p95: calculatePercentile(sortedMgas, 95),
          p99: calculatePercentile(sortedMgas, 99),
          last: mgasValues[0],
        }
      } else {
        clientMgasMinMax[client] = { min: 0, max: 0 }
        clientMgasStats[client] = {}
      }
    }

    // Calculate min/max across all runs
    let minDuration = Infinity
    let maxDuration = -Infinity
    let minMgas = Infinity
    let maxMgas = -Infinity

    for (const run of runs) {
      const stats = getIndexAggregatedStats(run, stepFilter)
      minDuration = Math.min(minDuration, stats.duration)
      maxDuration = Math.max(maxDuration, stats.duration)

      const mgas = calculateMGasPerSec(stats.gasUsed, stats.gasUsedDuration)
      if (mgas !== undefined) {
        minMgas = Math.min(minMgas, mgas)
        maxMgas = Math.max(maxMgas, mgas)
      }
    }

    if (minMgas === Infinity) minMgas = 0
    if (maxMgas === -Infinity) maxMgas = 0

    // Sort clients alphabetically
    const clients = Object.keys(clientRuns).sort()

    return {
      clientRuns,
      minDuration,
      maxDuration,
      minMgas,
      maxMgas,
      clientDurationMinMax,
      clientMgasMinMax,
      clientDurationStats,
      clientMgasStats,
      clients,
    }
  }, [runs, stepFilter])

  const getColorForRun = (run: IndexEntry) => {
    const stats = getIndexAggregatedStats(run, stepFilter)
    if (metricMode === 'mgas') {
      const mgas = calculateMGasPerSec(stats.gasUsed, stats.gasUsedDuration)
      if (mgas === undefined) return COLORS[2] // middle color if no data

      if (colorNormalization === 'client') {
        const { min, max } = clientMgasMinMax[run.instance.client]
        return getColorByNormalizedValue(mgas, min, max, true)
      }
      return getColorByNormalizedValue(mgas, minMgas, maxMgas, true)
    } else {
      if (colorNormalization === 'client') {
        const { min, max } = clientDurationMinMax[run.instance.client]
        return getColorByNormalizedValue(stats.duration, min, max, false)
      }
      return getColorByNormalizedValue(stats.duration, minDuration, maxDuration, false)
    }
  }

  const handleRunClick = (runId: string) => {
    navigate({
      to: '/runs/$runId',
      params: { runId },
    })
  }

  const handleMouseEnter = (run: IndexEntry, event: React.MouseEvent) => {
    const rect = event.currentTarget.getBoundingClientRect()
    setTooltip({
      run,
      x: rect.left + rect.width / 2,
      y: rect.top,
    })
  }

  const handleMouseLeave = () => {
    setTooltip(null)
  }

  if (runs.length === 0) {
    return null
  }

  return (
    <div className="relative">
      <div className="mb-3 flex items-center justify-end gap-4">
        {/* Metric mode toggle */}
        <div className="flex items-center gap-2">
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">Metric:</span>
          <div className="flex items-center gap-1 rounded-sm bg-gray-100 p-0.5 dark:bg-gray-700">
            <button
              onClick={() => setMetricMode('mgas')}
              className={clsx(
                'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                metricMode === 'mgas'
                  ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                  : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
              )}
            >
              MGas/s
            </button>
            <button
              onClick={() => setMetricMode('duration')}
              className={clsx(
                'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                metricMode === 'duration'
                  ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                  : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
              )}
            >
              Duration
            </button>
          </div>
        </div>

        {/* Color normalization toggle */}
        {onColorNormalizationChange && (
          <div className="flex items-center gap-2">
            <span className="text-xs/5 text-gray-500 dark:text-gray-400">Colors:</span>
            <div className="flex items-center gap-1 rounded-sm bg-gray-100 p-0.5 dark:bg-gray-700">
              <button
                onClick={() => onColorNormalizationChange('suite')}
                className={clsx(
                  'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                  colorNormalization === 'suite'
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                    : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                )}
              >
                Suite
              </button>
              <button
                onClick={() => onColorNormalizationChange('client')}
                className={clsx(
                  'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                  colorNormalization === 'client'
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                    : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                )}
              >
                Per Client
              </button>
            </div>
          </div>
        )}
      </div>

      <div className="flex flex-col gap-2">
        {/* Stats header */}
        <div className="flex items-center gap-3">
          <div className="w-28 shrink-0" />
          <div className="flex-1" />
          <div className="flex shrink-0 gap-3 border-l border-transparent pl-3 font-mono text-xs/5 font-medium text-gray-400 dark:text-gray-500">
            <span className="w-10 text-center">Min</span>
            <span className="w-10 text-center">Max</span>
            <span className="w-10 text-center">P95</span>
            <span className="w-10 text-center">P99</span>
            <span className="w-10 text-center">Mean</span>
            <span className="w-10 text-center">Last</span>
          </div>
        </div>
        {clients.map((client) => {
          const stats = metricMode === 'mgas' ? clientMgasStats[client] : clientDurationStats[client]
          const formatValue = (v?: number) => {
            if (v === undefined) return '-'
            if (metricMode === 'mgas') return v.toFixed(1)
            return formatDurationCompact(v)
          }
          return (
            <div key={client} className="flex items-center gap-3">
              <div className="w-28 shrink-0">
                <ClientBadge client={client} />
              </div>
              <div className="flex flex-1 gap-1">
                {clientRuns[client].map((run) => {
                  const runStats = getIndexAggregatedStats(run, stepFilter)
                  const completed = isRunCompleted(run)
                  return (
                    <button
                      key={run.run_id}
                      onClick={() => handleRunClick(run.run_id)}
                      onMouseEnter={(e) => handleMouseEnter(run, e)}
                      onMouseLeave={handleMouseLeave}
                      className={clsx(
                        'relative size-5 shrink-0 cursor-pointer rounded-xs transition-all hover:scale-110 hover:ring-2 hover:ring-gray-400 dark:hover:ring-gray-500',
                        runStats.fail > 0 && completed && 'ring-1 ring-red-500',
                        !completed && 'ring-2 ring-red-600 dark:ring-red-500',
                      )}
                      style={{ backgroundColor: completed ? getColorForRun(run) : '#6b7280' }}
                      title={`${formatTimestamp(run.timestamp)} - ${completed ? formatDurationMinSec(runStats.duration) : run.status}`}
                    >
                      {!completed && (
                        <svg className="absolute inset-0 size-5 text-red-600 dark:text-red-400" viewBox="0 0 20 20" fill="currentColor">
                          <path d="M4 4l12 12M4 16L16 4" stroke="currentColor" strokeWidth="2" fill="none" />
                        </svg>
                      )}
                    </button>
                  )
                })}
              </div>
              <div className="flex shrink-0 gap-3 border-l border-gray-200 pl-3 font-mono text-xs/5 text-gray-500 dark:border-gray-700 dark:text-gray-400">
                <span className="w-10 text-center">{formatValue(stats.min)}</span>
                <span className="w-10 text-center">{formatValue(stats.max)}</span>
                <span className="w-10 text-center">{formatValue(stats.p95)}</span>
                <span className="w-10 text-center">{formatValue(stats.p99)}</span>
                <span className="w-10 text-center">{formatValue(stats.mean)}</span>
                <span className="w-10 text-center">{formatValue(stats.last)}</span>
              </div>
            </div>
          )
        })}
      </div>

      {/* Legend */}
      <div className="mt-4 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs/5 text-gray-500 dark:text-gray-400">
        <span>Recent → Older</span>
        <span className="flex items-center gap-1">
          <span>Fast</span>
          <span className="flex gap-0.5">
            {COLORS.map((color, i) => (
              <span key={i} className="size-3 rounded-xs" style={{ backgroundColor: color }} />
            ))}
          </span>
          <span>Slow</span>
        </span>
        <span>
          <span className="mr-1 inline-block size-3 rounded-xs ring-1 ring-red-500" style={{ backgroundColor: COLORS[2] }} />
          Has failures
        </span>
        <span className="flex items-center gap-1">
          <span className="relative inline-block size-3 rounded-xs bg-gray-500 ring-2 ring-red-600">
            <svg className="absolute inset-0 size-3 text-red-600" viewBox="0 0 12 12">
              <path d="M2 2l8 8M2 10L10 2" stroke="currentColor" strokeWidth="1.5" fill="none" />
            </svg>
          </span>
          Interrupted
        </span>
      </div>

      {/* Tooltip */}
      {tooltip && (
        <div
          className={clsx(
            'pointer-events-none fixed z-50 rounded-sm px-3 py-2 text-xs/5 shadow-lg',
            isDark ? 'bg-gray-800 text-gray-100' : 'bg-white text-gray-900 ring-1 ring-gray-200',
          )}
          style={{
            left: tooltip.x,
            top: tooltip.y - 8,
            transform: 'translate(-50%, -100%)',
          }}
        >
          {(() => {
            const tooltipStats = getIndexAggregatedStats(tooltip.run, stepFilter)
            const completed = isRunCompleted(tooltip.run)
            return (
              <div className="flex flex-col gap-1">
                <div className="font-medium">{tooltip.run.instance.client}</div>
                <div>{formatTimestamp(tooltip.run.timestamp)}</div>
                {!completed && (
                  <div className="flex items-center gap-1 font-medium text-red-600 dark:text-red-400">
                    <svg className="size-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z" />
                    </svg>
                    {tooltip.run.status === 'container_died' ? 'Container Died' : 'Cancelled'}
                  </div>
                )}
                {tooltip.run.termination_reason && (
                  <div className="text-red-500 dark:text-red-400" style={{ maxWidth: '200px' }}>
                    {tooltip.run.termination_reason}
                  </div>
                )}
                <div>Duration: {formatDurationMinSec(tooltipStats.duration)}</div>
                {(() => {
                  const mgas = calculateMGasPerSec(tooltipStats.gasUsed, tooltipStats.gasUsedDuration)
                  return mgas !== undefined ? <div>MGas/s: {mgas.toFixed(2)}</div> : null
                })()}
                <div className="truncate text-gray-500 dark:text-gray-400" style={{ maxWidth: '200px' }}>
                  {tooltip.run.instance.image}
                </div>
                <div className="flex gap-2">
                  <span className="text-green-600 dark:text-green-400">{tooltipStats.success} passed</span>
                  {tooltipStats.fail > 0 && (
                    <span className="text-red-600 dark:text-red-400">{tooltipStats.fail} failed</span>
                  )}
                </div>
                <div className="mt-1 text-gray-400 dark:text-gray-500">Click for details</div>
              </div>
            )
          })()}
        </div>
      )}
    </div>
  )
}
