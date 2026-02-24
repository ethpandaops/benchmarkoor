import { useState, useMemo } from 'react'
import { useNavigate } from '@tanstack/react-router'
import clsx from 'clsx'
import { type IndexEntry, type IndexStepType, getIndexAggregatedStats } from '@/api/types'
import { formatTimestamp } from '@/utils/date'

const COLORS = [
  '#22c55e', // green - best
  '#84cc16', // lime
  '#eab308', // yellow
  '#f97316', // orange
  '#ef4444', // red - worst
]

function getColorByNormalizedValue(value: number, min: number, max: number, higherIsBetter: boolean): string {
  if (max === min) return COLORS[2]
  let normalized = (value - min) / (max - min)
  if (higherIsBetter) normalized = 1 - normalized
  const level = Math.min(4, Math.floor(normalized * 5))
  return COLORS[level]
}

function calculateMGasPerSec(gasUsed: number, gasUsedDuration: number): number | undefined {
  if (gasUsedDuration <= 0 || gasUsed <= 0) return undefined
  return (gasUsed * 1000) / gasUsedDuration
}

function formatDurationMinSec(nanoseconds: number): string {
  const seconds = nanoseconds / 1_000_000_000
  if (seconds < 60) return `${seconds.toFixed(1)}s`
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = Math.floor(seconds % 60)
  return `${minutes}m ${remainingSeconds}s`
}

function isRunCompleted(run: IndexEntry): boolean {
  return !run.status || run.status === 'completed'
}

const MAX_RUNS = 38

interface TooltipData {
  run: IndexEntry
  x: number
  y: number
}

interface ClientRunsStripProps {
  runs: IndexEntry[]
  currentRunId: string
  stepFilter: IndexStepType[]
}

export function ClientRunsStrip({ runs, currentRunId, stepFilter }: ClientRunsStripProps) {
  const navigate = useNavigate()
  const [tooltip, setTooltip] = useState<TooltipData | null>(null)

  const { displayRuns, minMgas, maxMgas } = useMemo(() => {
    const sorted = [...runs].sort((a, b) => b.timestamp - a.timestamp)
    const displayRuns = sorted.slice(0, MAX_RUNS)

    let minMgas = Infinity
    let maxMgas = -Infinity
    for (const run of displayRuns) {
      const stats = getIndexAggregatedStats(run, stepFilter)
      const mgas = calculateMGasPerSec(stats.gasUsed, stats.gasUsedDuration)
      if (mgas !== undefined) {
        minMgas = Math.min(minMgas, mgas)
        maxMgas = Math.max(maxMgas, mgas)
      }
    }
    if (minMgas === Infinity) minMgas = 0
    if (maxMgas === -Infinity) maxMgas = 0

    return { displayRuns, minMgas, maxMgas }
  }, [runs, stepFilter])

  if (displayRuns.length <= 1) return null

  return (
    <div className="relative flex items-center gap-3 rounded-sm bg-white px-4 py-3 shadow-xs dark:bg-gray-800">
      <span className="shrink-0 text-xs/5 text-gray-400 dark:text-gray-500">Recent</span>
      <div className="flex gap-1">
        {displayRuns.map((run) => {
          const stats = getIndexAggregatedStats(run, stepFilter)
          const completed = isRunCompleted(run)
          const mgas = calculateMGasPerSec(stats.gasUsed, stats.gasUsedDuration)
          const color = completed && mgas !== undefined
            ? getColorByNormalizedValue(mgas, minMgas, maxMgas, true)
            : completed ? COLORS[2] : '#6b7280'
          const isCurrent = run.run_id === currentRunId

          return (
            <button
              key={run.run_id}
              onClick={() => navigate({ to: '/runs/$runId', params: { runId: run.run_id } })}
              onMouseEnter={(e) => {
                const rect = e.currentTarget.getBoundingClientRect()
                setTooltip({ run, x: rect.left + rect.width / 2, y: rect.top })
              }}
              onMouseLeave={() => setTooltip(null)}
              className={clsx(
                'relative size-5 shrink-0 cursor-pointer rounded-xs transition-all hover:scale-110',
                isCurrent && 'ring-2 ring-blue-500',
                !isCurrent && 'hover:ring-2 hover:ring-gray-400 dark:hover:ring-gray-500',
                run.tests.tests_total - run.tests.tests_passed > 0 && completed && !isCurrent && 'ring-2 ring-inset ring-orange-500',
                !completed && !isCurrent && 'ring-2 ring-inset ring-red-600 dark:ring-red-500',
              )}
              style={{ backgroundColor: color }}
            >
              {completed && run.tests.tests_total - run.tests.tests_passed > 0 && (
                <svg className="absolute inset-0 size-5" viewBox="0 0 20 20" fill="none">
                  <text x="10" y="15" textAnchor="middle" fill="white" fontSize="13" fontWeight="bold" fontFamily="system-ui">!</text>
                </svg>
              )}
              {!completed && (
                <svg className="absolute inset-0 size-5 text-red-600 dark:text-red-400" viewBox="0 0 20 20">
                  <path d="M4 4l12 12M4 16L16 4" stroke="currentColor" strokeWidth="2" fill="none" />
                </svg>
              )}
            </button>
          )
        })}
      </div>
      <span className="shrink-0 text-xs/5 text-gray-400 dark:text-gray-500">Older</span>
      <div className="ml-2 flex items-center gap-1 border-l border-gray-200 pl-3 dark:border-gray-700">
        <span className="flex gap-0.5">
          {COLORS.map((color, i) => (
            <span key={i} className="size-3 rounded-xs" style={{ backgroundColor: color }} />
          ))}
        </span>
        <span className="ml-1 text-xs/5 text-gray-400 dark:text-gray-500">MGas/s</span>
      </div>
      <div className="ml-2 flex items-center gap-1 border-l border-gray-200 pl-3 dark:border-gray-700">
        <span className="size-3 rounded-xs bg-blue-500 ring-2 ring-blue-500" />
        <span className="text-xs/5 text-gray-400 dark:text-gray-500">Current</span>
      </div>

      {tooltip && (() => {
        const stats = getIndexAggregatedStats(tooltip.run, stepFilter)
        const completed = isRunCompleted(tooltip.run)
        const mgas = calculateMGasPerSec(stats.gasUsed, stats.gasUsedDuration)
        return (
          <div
            className="pointer-events-none fixed z-50 rounded-sm bg-white px-3 py-2 text-xs/5 shadow-lg ring-1 ring-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:ring-0"
            style={{ left: tooltip.x, top: tooltip.y - 8, transform: 'translate(-50%, -100%)' }}
          >
            <div className="flex flex-col gap-1">
              <div className="font-medium">{tooltip.run.instance.client}</div>
              <div>{formatTimestamp(tooltip.run.timestamp)}</div>
              {!completed && (
                <div className="font-medium text-red-600 dark:text-red-400">
                  {tooltip.run.status === 'container_died' ? 'Container Died' : 'Cancelled'}
                </div>
              )}
              <div>Duration: {formatDurationMinSec(stats.duration)}</div>
              {mgas !== undefined && <div>MGas/s: {mgas.toFixed(2)}</div>}
              <div className="flex gap-2">
                <span className="text-green-600 dark:text-green-400">
                  {tooltip.run.tests.tests_passed} passed
                </span>
                {tooltip.run.tests.tests_total - tooltip.run.tests.tests_passed > 0 && (
                  <span className="text-red-600 dark:text-red-400">
                    {tooltip.run.tests.tests_total - tooltip.run.tests.tests_passed} failed
                  </span>
                )}
                <span className="text-gray-500 dark:text-gray-400">
                  ({tooltip.run.tests.tests_total} total)
                </span>
              </div>
              {tooltip.run.run_id !== currentRunId && (
                <div className="text-gray-400 dark:text-gray-500">Click for details</div>
              )}
            </div>
          </div>
        )
      })()}
    </div>
  )
}
