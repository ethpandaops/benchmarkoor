import { useCallback, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { ChevronLeft, ChevronRight } from 'lucide-react'
import clsx from 'clsx'
import { type IndexEntry, type IndexStepType, getIndexAggregatedStats } from '@/api/types'
import { formatTimestamp } from '@/utils/date'
import { ColorScaleLegend } from '@/components/shared/ColorScaleLegend'
import { COLORS, NEUTRAL_COLOR, createColorScale, formatMgasStepRange } from '@/utils/runColorScale'
import { positionTooltip } from '@/utils/tooltipPosition'

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

// Swatch width and the gap between two swatches, in px. Together they say
// how many runs fit on one row of the strip: size-5 and gap-1 below.
const SWATCH_PX = 20
const GAP_PX = 4

interface TooltipData {
  run: IndexEntry
  anchor: DOMRect
}

interface ClientRunsStripProps {
  runs: IndexEntry[]
  currentRunId: string
  stepFilter: IndexStepType[]
  selectable?: boolean
  selectedRunIds?: Set<string>
  onSelectionChange?: (runId: string, selected: boolean) => void
}

export function ClientRunsStrip({ runs, currentRunId, stepFilter, selectable = false, selectedRunIds, onSelectionChange }: ClientRunsStripProps) {
  const navigate = useNavigate()
  const [tooltip, setTooltip] = useState<TooltipData | null>(null)

  // The tooltip is sized by its contents: a run ID, and however many metadata
  // labels the run carries. Its size is only known once rendered, so it
  // renders hidden, and this measures it and places it before the paint.
  // Guessing instead let the first swatch of the strip, which sits near the
  // left edge, hang half its tooltip outside the window.
  const tooltipRef = useRef<HTMLDivElement>(null)

  useLayoutEffect(() => {
    const el = tooltipRef.current
    if (!tooltip || !el) return

    const { width, height } = el.getBoundingClientRect()
    const { left, top } = positionTooltip(
      tooltip.anchor,
      { width, height },
      { width: window.innerWidth, height: window.innerHeight },
    )

    el.style.left = `${left}px`
    el.style.top = `${top}px`
    el.style.visibility = 'visible'
  }, [tooltip])

  // The strip never wraps: it shows one page of runs, as many as fit on
  // one row, so the legend stays on the same line at every width.
  // A callback ref attaches the observer to the strip node itself, so
  // it also measures a strip that appears later, when the live view
  // gets its first peer run.
  const [fitCount, setFitCount] = useState(1)

  const stripRef = useCallback((strip: HTMLDivElement | null) => {
    if (!strip) return
    const measure = () => {
      const count = Math.floor((strip.clientWidth + GAP_PX) / (SWATCH_PX + GAP_PX))
      setFitCount(Math.max(1, count))
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(strip)
    return () => observer.disconnect()
  }, [])

  const sorted = useMemo(() => [...runs].sort((a, b) => b.timestamp - a.timestamp), [runs])

  // The arrows move the page. The page is anchored to the run at its
  // newest end, not to an index, so a page holds still when the live
  // view adds newer runs. The page is also tied to the current run, so
  // a new current run starts over: the page then begins at the newest
  // run, or slides down so the current run is the oldest one on the
  // strip.
  const [page, setPage] = useState<{ runId: string; anchorId: string } | null>(null)
  const setPageStart = (start: number) => setPage({ runId: currentRunId, anchorId: sorted[start].run_id })

  const maxStart = Math.max(0, sorted.length - fitCount)
  const currentIndex = sorted.findIndex((run) => run.run_id === currentRunId)
  const autoStart = Math.max(0, currentIndex - fitCount + 1)
  const anchorIndex = page?.runId === currentRunId ? sorted.findIndex((run) => run.run_id === page.anchorId) : -1
  const start = Math.min(anchorIndex >= 0 ? anchorIndex : autoStart, maxStart)

  // Same scale as the suite page's per-client heatmap: a run's colour
  // says how far it sits below the best of the strip, so a strip of
  // near-identical runs stays green instead of spanning the rainbow.
  const { displayRuns, scale } = useMemo(() => {
    const displayRuns = sorted.slice(start, start + fitCount)

    const mgasValues: number[] = []
    for (const run of displayRuns) {
      if (!isRunCompleted(run)) continue
      const stats = getIndexAggregatedStats(run, stepFilter)
      const mgas = calculateMGasPerSec(stats.gasUsed, stats.gasUsedDuration)
      if (mgas !== undefined) mgasValues.push(mgas)
    }

    return { displayRuns, scale: createColorScale(mgasValues, true) }
  }, [sorted, start, fitCount, stepFilter])

  if (runs.length <= 1) return null

  const pageButtonClass = 'flex shrink-0 cursor-pointer items-center justify-center rounded-xs p-0.5 text-gray-400 transition-colors hover:bg-gray-100 hover:text-gray-600 disabled:cursor-not-allowed disabled:opacity-30 disabled:hover:bg-transparent dark:text-gray-500 dark:hover:bg-gray-700 dark:hover:text-gray-300'

  return (
    <div className="relative flex items-center gap-3 rounded-xs bg-white px-4 py-3 shadow-xs dark:bg-gray-800">
      <div className="flex min-w-0 flex-1 items-center gap-2">
        <button
          type="button"
          onClick={() => setPageStart(Math.max(0, start - fitCount))}
          disabled={start === 0}
          className={pageButtonClass}
          title="Newer runs"
        >
          <ChevronLeft className="size-4" />
        </button>
        <div ref={stripRef} className="flex min-w-0 flex-1 gap-1">
          {displayRuns.map((run) => {
            const stats = getIndexAggregatedStats(run, stepFilter)
            const completed = isRunCompleted(run)
            const mgas = calculateMGasPerSec(stats.gasUsed, stats.gasUsedDuration)
            const color = completed && mgas !== undefined
              ? scale.color(mgas)
              : completed ? NEUTRAL_COLOR : '#6b7280'
            const isCurrent = run.run_id === currentRunId

            return (
              <button
                key={run.run_id}
                onClick={() => {
                  if (selectable) {
                    onSelectionChange?.(run.run_id, !selectedRunIds?.has(run.run_id))
                  } else {
                    navigate({ to: '/runs/$runId', params: { runId: run.run_id } })
                  }
                }}
                onMouseEnter={(e) => {
                  setTooltip({ run, anchor: e.currentTarget.getBoundingClientRect() })
                }}
                onMouseLeave={() => setTooltip(null)}
                className={clsx(
                  'relative size-5 shrink-0 cursor-pointer rounded-xs transition-all hover:scale-110',
                  selectable && selectedRunIds?.has(run.run_id) && 'scale-110 ring-2 ring-blue-500 dark:ring-blue-400',
                  !selectable && isCurrent && 'ring-2 ring-blue-500',
                  !selectable && !isCurrent && 'hover:ring-2 hover:ring-gray-400 dark:hover:ring-gray-500',
                  run.tests.tests_total - run.tests.tests_passed > 0 && completed && !isCurrent && !selectedRunIds?.has(run.run_id) && 'ring-2 ring-inset ring-orange-500',
                  !completed && !isCurrent && !selectedRunIds?.has(run.run_id) && 'ring-2 ring-inset ring-red-600 dark:ring-red-500',
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
        <button
          type="button"
          onClick={() => setPageStart(Math.min(maxStart, start + fitCount))}
          disabled={start >= maxStart}
          className={pageButtonClass}
          title="Older runs"
        >
          <ChevronRight className="size-4" />
        </button>
        <span className="hidden shrink-0 text-xs/5 text-gray-400 tabular-nums sm:inline dark:text-gray-500">
          {start + 1}–{start + displayRuns.length} of {sorted.length}
        </span>
      </div>
      <div className="flex shrink-0 items-center gap-3">
        <div className="flex items-center gap-1 border-l border-gray-200 pl-3 text-xs/5 text-gray-400 dark:border-gray-700 dark:text-gray-500">
          <ColorScaleLegend
            colors={COLORS}
            endLabel="MGas/s"
            title="MGas/s buckets"
            stepRange={(step) => formatMgasStepRange(scale, step)}
            note="Each run is measured against the best run on this page of the strip. A page that spreads wider than 15% gets a wider scale."
          />
        </div>
        <div className="flex items-center gap-1 border-l border-gray-200 pl-3 dark:border-gray-700">
          <span className="size-3 rounded-xs bg-blue-500 ring-2 ring-blue-500" />
          <span className="text-xs/5 text-gray-400 dark:text-gray-500">Current</span>
        </div>
      </div>

      {tooltip && (() => {
        const stats = getIndexAggregatedStats(tooltip.run, stepFilter)
        const completed = isRunCompleted(tooltip.run)
        const mgas = calculateMGasPerSec(stats.gasUsed, stats.gasUsedDuration)
        const labelEntries = Object.entries(tooltip.run.metadata ?? {})
        return (
          <div
            ref={tooltipRef}
            className="pointer-events-none fixed z-50 max-w-[90vw] rounded-sm bg-white px-3 py-2 text-xs/5 shadow-lg ring-1 ring-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:ring-0"
            style={{ left: 0, top: 0, visibility: 'hidden' }}
          >
            <div className="flex flex-col gap-1">
              <div className="font-medium">{tooltip.run.instance.client}</div>
              <div className="font-mono text-[11px] text-gray-500 dark:text-gray-400">{tooltip.run.run_id}</div>
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
              {labelEntries.length > 0 && (
                <div className="mt-1 flex flex-col gap-0.5 border-t border-gray-200 pt-1 dark:border-gray-700">
                  {labelEntries.map(([k, v]) => (
                    <div key={k} className="text-[11px] text-gray-500 dark:text-gray-400">
                      <span className="font-medium">{k}:</span> {v}
                    </div>
                  ))}
                </div>
              )}
              {selectable ? (
                <div className="text-gray-400 dark:text-gray-500">Click to select</div>
              ) : tooltip.run.run_id !== currentRunId ? (
                <div className="text-gray-400 dark:text-gray-500">Click for details</div>
              ) : null}
            </div>
          </div>
        )
      })()}
    </div>
  )
}
