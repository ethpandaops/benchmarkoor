import { useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import clsx from 'clsx'
import type { IndexEntry } from '@/api/types'
import { formatTimestamp } from '@/utils/date'
import { ClientBadge } from '@/components/shared/ClientBadge'

const MAX_RUNS_PER_CLIENT = 20

// 5-level discrete color scale (green to red)
const DURATION_COLORS = [
  '#22c55e', // green - fastest 20%
  '#84cc16', // lime - 20-40%
  '#eab308', // yellow - 40-60%
  '#f97316', // orange - 60-80%
  '#ef4444', // red - slowest 20%
]

function getDurationColor(duration: number, minDuration: number, maxDuration: number): string {
  if (maxDuration === minDuration) return DURATION_COLORS[2] // middle color if all same
  const normalized = (duration - minDuration) / (maxDuration - minDuration)
  const level = Math.min(4, Math.floor(normalized * 5))
  return DURATION_COLORS[level]
}

function formatDurationMinSec(nanoseconds: number): string {
  const seconds = nanoseconds / 1_000_000_000
  if (seconds < 60) return `${seconds.toFixed(1)}s`
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = Math.floor(seconds % 60)
  return `${minutes}m ${remainingSeconds}s`
}

export type ColorNormalization = 'suite' | 'client'

interface RunsHeatmapProps {
  runs: IndexEntry[]
  isDark: boolean
  colorNormalization?: ColorNormalization
  onColorNormalizationChange?: (mode: ColorNormalization) => void
}

interface TooltipData {
  run: IndexEntry
  x: number
  y: number
}

export function RunsHeatmap({ runs, isDark, colorNormalization = 'suite', onColorNormalizationChange }: RunsHeatmapProps) {
  const navigate = useNavigate()
  const [tooltip, setTooltip] = useState<TooltipData | null>(null)

  const { clientRuns, minDuration, maxDuration, clientMinMax, clients } = useMemo(() => {
    // Group runs by client
    const grouped: Record<string, IndexEntry[]> = {}
    for (const run of runs) {
      const client = run.instance.client
      if (!grouped[client]) grouped[client] = []
      grouped[client].push(run)
    }

    // Sort each client's runs by timestamp (oldest first) and take last 20
    const clientRuns: Record<string, IndexEntry[]> = {}
    const clientMinMax: Record<string, { min: number; max: number }> = {}
    for (const [client, clientRunsAll] of Object.entries(grouped)) {
      const sorted = [...clientRunsAll].sort((a, b) => a.timestamp - b.timestamp)
      clientRuns[client] = sorted.slice(-MAX_RUNS_PER_CLIENT)

      // Calculate per-client min/max
      let min = Infinity
      let max = -Infinity
      for (const run of clientRuns[client]) {
        min = Math.min(min, run.tests.duration)
        max = Math.max(max, run.tests.duration)
      }
      clientMinMax[client] = { min, max }
    }

    // Calculate min/max duration across all runs
    let minDuration = Infinity
    let maxDuration = -Infinity
    for (const run of runs) {
      minDuration = Math.min(minDuration, run.tests.duration)
      maxDuration = Math.max(maxDuration, run.tests.duration)
    }

    // Sort clients alphabetically
    const clients = Object.keys(clientRuns).sort()

    return { clientRuns, minDuration, maxDuration, clientMinMax, clients }
  }, [runs])

  const getColorForRun = (run: IndexEntry) => {
    if (colorNormalization === 'client') {
      const { min, max } = clientMinMax[run.instance.client]
      return getDurationColor(run.tests.duration, min, max)
    }
    return getDurationColor(run.tests.duration, minDuration, maxDuration)
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
      <div className="mb-3 flex items-center justify-between">
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Recent Runs by Client</h3>
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
        {clients.map((client) => (
          <div key={client} className="flex items-center gap-3">
            <div className="w-28 shrink-0">
              <ClientBadge client={client} />
            </div>
            <div className="flex gap-1">
              {clientRuns[client].map((run) => (
                <button
                  key={run.run_id}
                  onClick={() => handleRunClick(run.run_id)}
                  onMouseEnter={(e) => handleMouseEnter(run, e)}
                  onMouseLeave={handleMouseLeave}
                  className={clsx(
                    'size-5 cursor-pointer rounded-xs transition-all hover:scale-110 hover:ring-2 hover:ring-gray-400 dark:hover:ring-gray-500',
                    run.tests.fail > 0 && 'ring-1 ring-red-500',
                  )}
                  style={{ backgroundColor: getColorForRun(run) }}
                  title={`${formatTimestamp(run.timestamp)} - ${formatDurationMinSec(run.tests.duration)}`}
                />
              ))}
            </div>
          </div>
        ))}
      </div>

      {/* Legend */}
      <div className="mt-4 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs/5 text-gray-500 dark:text-gray-400">
        <span>Older → Recent</span>
        <span className="flex items-center gap-1">
          <span>Fast</span>
          <span className="flex gap-0.5">
            {DURATION_COLORS.map((color, i) => (
              <span key={i} className="size-3 rounded-xs" style={{ backgroundColor: color }} />
            ))}
          </span>
          <span>Slow</span>
        </span>
        <span>
          <span className="mr-1 inline-block size-3 rounded-xs ring-1 ring-red-500" style={{ backgroundColor: DURATION_COLORS[2] }} />
          Has failures
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
          <div className="flex flex-col gap-1">
            <div className="font-medium">{tooltip.run.instance.client}</div>
            <div>{formatTimestamp(tooltip.run.timestamp)}</div>
            <div>Duration: {formatDurationMinSec(tooltip.run.tests.duration)}</div>
            <div className="truncate text-gray-500 dark:text-gray-400" style={{ maxWidth: '200px' }}>
              {tooltip.run.instance.image}
            </div>
            <div className="flex gap-2">
              <span className="text-green-600 dark:text-green-400">{tooltip.run.tests.success} passed</span>
              {tooltip.run.tests.fail > 0 && (
                <span className="text-red-600 dark:text-red-400">{tooltip.run.tests.fail} failed</span>
              )}
            </div>
            <div className="mt-1 text-gray-400 dark:text-gray-500">Click for details</div>
          </div>
        </div>
      )}
    </div>
  )
}
