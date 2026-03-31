import { useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import clsx from 'clsx'
import { AlertTriangle, GitCompareArrows } from 'lucide-react'
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

/**
 * Create a percentile-based color mapper. Values are split into equal-sized
 * quintiles so the color spread is balanced regardless of outliers.
 */
function createColorScale(values: number[], higherIsBetter: boolean): (value: number) => string {
  if (values.length === 0) return () => COLORS[2]
  const sorted = [...values].sort((a, b) => a - b)
  return (value: number) => {
    // Binary search for rank position
    let lo = 0
    let hi = sorted.length
    while (lo < hi) {
      const mid = (lo + hi) >>> 1
      if (sorted[mid] < value) lo = mid + 1
      else hi = mid
    }
    let percentile = lo / sorted.length
    if (higherIsBetter) percentile = 1 - percentile
    const level = Math.min(4, Math.floor(percentile * 5))
    return COLORS[level]
  }
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
  /** When set, runs are grouped by this label key (or 'instance_id') before client grouping. */
  groupBy?: string
  /** Called with the runs of a group when the per-group compare button is clicked. */
  onCompareGroup?: (runs: IndexEntry[]) => void
  /** Called with a client name to compare its latest successful run across all groups. */
  onCompareClientAcrossGroups?: (client: string) => void
  isDark: boolean
  colorNormalization?: ColorNormalization
  onColorNormalizationChange?: (mode: ColorNormalization) => void
  metricMode?: MetricMode
  onMetricModeChange?: (mode: MetricMode) => void
  stepFilter?: IndexStepType[]
  selectable?: boolean
  selectedRunIds?: Set<string>
  onSelectionChange?: (runId: string, selected: boolean) => void
}

interface GroupSection {
  label: string
  clients: string[]
  clientRuns: Record<string, IndexEntry[]>
  clientDurationStats: Record<string, ClientStats>
  clientMgasStats: Record<string, ClientStats>
  clientDurationScales: Record<string, (v: number) => string>
  clientMgasScales: Record<string, (v: number) => string>
}

interface TooltipData {
  run: IndexEntry
  x: number
  y: number
}

export function RunsHeatmap({
  runs,
  groupBy,
  onCompareGroup,
  onCompareClientAcrossGroups,
  isDark,
  colorNormalization = 'suite',
  onColorNormalizationChange,
  metricMode: controlledMetricMode,
  onMetricModeChange,
  stepFilter = ALL_INDEX_STEP_TYPES,
  selectable = false,
  selectedRunIds,
  onSelectionChange,
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
    clientDurationStats,
    clientMgasStats,
    clients,
    suiteDurationScale,
    suiteMgasScale,
    clientDurationScales,
    clientMgasScales,
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
    const clientDurationStats: Record<string, ClientStats> = {}
    const clientMgasStats: Record<string, ClientStats> = {}
    const clientDurationScales: Record<string, (v: number) => string> = {}
    const clientMgasScales: Record<string, (v: number) => string> = {}

    const allDurations: number[] = []
    const allMgas: number[] = []

    for (const [client, clientRunsAll] of Object.entries(grouped)) {
      const sorted = [...clientRunsAll].sort((a, b) => b.timestamp - a.timestamp)
      clientRuns[client] = sorted.slice(0, MAX_RUNS_PER_CLIENT)

      // Calculate duration stats
      const durations = clientRuns[client].map((r) => getIndexAggregatedStats(r, stepFilter).duration)
      const sortedDurations = [...durations].sort((a, b) => a - b)
      const durationSum = durations.reduce((acc, d) => acc + d, 0)
      allDurations.push(...durations)

      clientDurationStats[client] = {
        min: sortedDurations[0],
        max: sortedDurations[sortedDurations.length - 1],
        mean: durationSum / durations.length,
        p95: calculatePercentile(sortedDurations, 95),
        p99: calculatePercentile(sortedDurations, 99),
        last: durations[0],
      }

      clientDurationScales[client] = createColorScale(durations, false)

      // Calculate MGas/s stats
      const mgasValues = clientRuns[client]
        .map((r) => {
          const stats = getIndexAggregatedStats(r, stepFilter)
          return calculateMGasPerSec(stats.gasUsed, stats.gasUsedDuration)
        })
        .filter((v): v is number => v !== undefined)
      allMgas.push(...mgasValues)

      if (mgasValues.length > 0) {
        const sortedMgas = [...mgasValues].sort((a, b) => a - b)
        const mgasSum = mgasValues.reduce((acc, v) => acc + v, 0)

        clientMgasStats[client] = {
          min: sortedMgas[0],
          max: sortedMgas[sortedMgas.length - 1],
          mean: mgasSum / mgasValues.length,
          p95: calculatePercentile(sortedMgas, 95),
          p99: calculatePercentile(sortedMgas, 99),
          last: mgasValues[0],
        }
      } else {
        clientMgasStats[client] = {}
      }

      clientMgasScales[client] = createColorScale(mgasValues, true)
    }

    // Sort clients alphabetically
    const clients = Object.keys(clientRuns).sort()

    return {
      clientRuns,
      clientDurationStats,
      clientMgasStats,
      clients,
      suiteDurationScale: createColorScale(allDurations, false),
      suiteMgasScale: createColorScale(allMgas, true),
      clientDurationScales,
      clientMgasScales,
    }
  }, [runs, stepFilter])

  // When groupBy is set, split runs into sections by label value
  const groupSections: GroupSection[] | null = useMemo(() => {
    if (!groupBy) return null

    // Partition runs by group value
    const grouped = new Map<string, IndexEntry[]>()
    for (const run of runs) {
      const value = groupBy === 'instance_id'
        ? run.instance.id
        : (run.metadata?.[groupBy] ?? '(none)')
      let list = grouped.get(value)
      if (!list) {
        list = []
        grouped.set(value, list)
      }
      list.push(run)
    }

    const sections: GroupSection[] = []
    for (const [label, groupRuns] of Array.from(grouped.entries()).sort(([a], [b]) => a.localeCompare(b))) {
      const byClient: Record<string, IndexEntry[]> = {}
      for (const run of groupRuns) {
        const c = run.instance.client
        if (!byClient[c]) byClient[c] = []
        byClient[c].push(run)
      }

      const sectionClientRuns: Record<string, IndexEntry[]> = {}
      const sectionDurationStats: Record<string, ClientStats> = {}
      const sectionMgasStats: Record<string, ClientStats> = {}
      const sectionDurationScales: Record<string, (v: number) => string> = {}
      const sectionMgasScales: Record<string, (v: number) => string> = {}

      for (const [c, cRuns] of Object.entries(byClient)) {
        const sorted = [...cRuns].sort((a, b) => b.timestamp - a.timestamp)
        sectionClientRuns[c] = sorted.slice(0, MAX_RUNS_PER_CLIENT)

        const durations = sectionClientRuns[c].map((r) => getIndexAggregatedStats(r, stepFilter).duration)
        const sortedD = [...durations].sort((a, b) => a - b)
        const dSum = durations.reduce((acc, d) => acc + d, 0)
        sectionDurationStats[c] = {
          min: sortedD[0], max: sortedD[sortedD.length - 1],
          mean: dSum / durations.length,
          p95: calculatePercentile(sortedD, 95), p99: calculatePercentile(sortedD, 99),
          last: durations[0],
        }
        sectionDurationScales[c] = createColorScale(durations, false)

        const mgasVals = sectionClientRuns[c]
          .map((r) => { const s = getIndexAggregatedStats(r, stepFilter); return calculateMGasPerSec(s.gasUsed, s.gasUsedDuration) })
          .filter((v): v is number => v !== undefined)
        if (mgasVals.length > 0) {
          const sortedM = [...mgasVals].sort((a, b) => a - b)
          const mSum = mgasVals.reduce((acc, v) => acc + v, 0)
          sectionMgasStats[c] = {
            min: sortedM[0], max: sortedM[sortedM.length - 1],
            mean: mSum / mgasVals.length,
            p95: calculatePercentile(sortedM, 95), p99: calculatePercentile(sortedM, 99),
            last: mgasVals[0],
          }
        } else {
          sectionMgasStats[c] = {}
        }
        sectionMgasScales[c] = createColorScale(mgasVals, true)
      }

      sections.push({
        label,
        clients: Object.keys(sectionClientRuns).sort(),
        clientRuns: sectionClientRuns,
        clientDurationStats: sectionDurationStats,
        clientMgasStats: sectionMgasStats,
        clientDurationScales: sectionDurationScales,
        clientMgasScales: sectionMgasScales,
      })
    }

    return sections
  }, [groupBy, runs, stepFilter])

  const getColorForRun = (
    run: IndexEntry,
    overrides?: {
      mgasScales: Record<string, (v: number) => string>
      durationScales: Record<string, (v: number) => string>
    },
  ) => {
    const stats = getIndexAggregatedStats(run, stepFilter)
    if (metricMode === 'mgas') {
      const mgas = calculateMGasPerSec(stats.gasUsed, stats.gasUsedDuration)
      if (mgas === undefined) return COLORS[2]

      if (colorNormalization === 'client') {
        const scales = overrides?.mgasScales ?? clientMgasScales
        return scales[run.instance.client]?.(mgas) ?? COLORS[2]
      }
      return suiteMgasScale(mgas)
    } else {
      if (colorNormalization === 'client') {
        const scales = overrides?.durationScales ?? clientDurationScales
        return scales[run.instance.client]?.(stats.duration) ?? COLORS[2]
      }
      return suiteDurationScale(stats.duration)
    }
  }

  const handleRunClick = (run: IndexEntry) => {
    if (selectable) {
      onSelectionChange?.(run.run_id, !selectedRunIds?.has(run.run_id))
    } else {
      navigate({
        to: '/runs/$runId',
        params: { runId: run.run_id },
      })
    }
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
      <div className="mb-3 flex flex-wrap items-center justify-end gap-3 sm:gap-4">
        {/* Metric mode toggle */}
        <div className="flex items-center gap-2">
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">Metric:</span>
          <div className="flex items-center gap-1 rounded-xs bg-gray-100 p-0.5 dark:bg-gray-700">
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
            <div className="flex items-center gap-1 rounded-xs bg-gray-100 p-0.5 dark:bg-gray-700">
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

      {(groupSections ?? [{ label: '', clients, clientRuns, clientDurationStats, clientMgasStats, clientDurationScales, clientMgasScales }]).map((section, sectionIdx) => (
        <div key={section.label || '_default'} className={clsx(sectionIdx > 0 && 'mt-4')}>
          {section.label && (
            <div className="mb-2 flex items-center gap-3">
              <div className="h-px grow bg-gray-200 dark:bg-gray-700" />
              <span className="inline-flex items-center gap-1.5 rounded-xs border border-gray-200 bg-gray-50 px-2.5 py-1 text-xs/5 font-medium text-gray-700 dark:border-gray-700 dark:bg-gray-800 dark:text-gray-300">
                <span className="font-semibold">{groupBy}</span>
                <span>=</span>
                <span>{section.label}</span>
              </span>
              {onCompareGroup && (
                <button
                  onClick={() => {
                    const allGroupRuns = section.clients.flatMap((c) => section.clientRuns[c])
                    onCompareGroup(allGroupRuns)
                  }}
                  className="flex shrink-0 cursor-pointer items-center justify-center rounded-xs p-1 shadow-xs ring-1 ring-inset transition-colors bg-white text-gray-500 ring-gray-300 hover:bg-gray-50 hover:text-gray-700 dark:bg-gray-800 dark:text-gray-400 dark:ring-gray-600 dark:hover:bg-gray-700 dark:hover:text-gray-200"
                  title="Compare latest successful run per client in this group"
                >
                  <GitCompareArrows className="size-3.5" />
                </button>
              )}
              <div className="h-px grow bg-gray-200 dark:bg-gray-700" />
            </div>
          )}
          <div className="flex flex-col gap-2">
            {/* Stats header */}
            {sectionIdx === 0 && (
              <div className="flex items-center gap-2 sm:gap-3">
                <div className={clsx('hidden shrink-0 sm:block', onCompareClientAcrossGroups && groupSections ? 'w-32' : 'w-28')} />
                <div className="flex-1" />
                <div className="hidden shrink-0 gap-3 border-l border-transparent pl-3 font-mono text-xs/5 font-medium text-gray-400 md:flex dark:text-gray-500">
                  <span className="w-10 text-center">Min</span>
                  <span className="w-10 text-center">Max</span>
                  <span className="w-10 text-center">P95</span>
                  <span className="w-10 text-center">P99</span>
                  <span className="w-10 text-center">Mean</span>
                  <span className="w-10 text-center">Last</span>
                </div>
              </div>
            )}
            {section.clients.map((client) => {
              const stats = metricMode === 'mgas' ? section.clientMgasStats[client] : section.clientDurationStats[client]
              const colorOverrides = groupSections ? { mgasScales: section.clientMgasScales, durationScales: section.clientDurationScales } : undefined
              const formatStatValue = (v?: number) => {
                if (v === undefined) return '-'
                if (metricMode === 'mgas') return v.toFixed(1)
                return formatDurationCompact(v)
              }
              return (
                <div key={`${section.label}-${client}`} className="flex items-center gap-2 sm:gap-3">
                  <div className={clsx('flex shrink-0 items-center gap-1', onCompareClientAcrossGroups && groupSections ? 'sm:w-32' : 'sm:w-28')}>
                    <span className="sm:hidden">
                      <ClientBadge client={client} hideLabel />
                    </span>
                    <span className="hidden sm:inline-flex">
                      <ClientBadge client={client} />
                    </span>
                    {onCompareClientAcrossGroups && groupSections && (
                      <button
                        onClick={() => onCompareClientAcrossGroups(client)}
                        className="flex shrink-0 items-center justify-center rounded-xs p-0.5 text-gray-400 transition-colors hover:text-gray-700 dark:text-gray-500 dark:hover:text-gray-200"
                        title={`Compare ${client} across groups`}
                      >
                        <GitCompareArrows className="size-3" />
                      </button>
                    )}
                  </div>
                  <div className="flex min-w-0 flex-1 flex-wrap gap-1">
                    {section.clientRuns[client].map((run) => {
                      const runStats = getIndexAggregatedStats(run, stepFilter)
                      const completed = isRunCompleted(run)
                      return (
                        <button
                          key={run.run_id}
                          onClick={() => handleRunClick(run)}
                          onMouseEnter={(e) => handleMouseEnter(run, e)}
                          onMouseLeave={handleMouseLeave}
                          className={clsx(
                            'relative size-5 shrink-0 cursor-pointer rounded-xs transition-all hover:scale-110 hover:ring-2 hover:ring-gray-400 dark:hover:ring-gray-500',
                            selectable && selectedRunIds?.has(run.run_id) && 'scale-110 ring-2 ring-blue-500 dark:ring-blue-400',
                            run.tests.tests_total - run.tests.tests_passed > 0 && completed && !selectedRunIds?.has(run.run_id) && 'ring-2 ring-inset ring-orange-500',
                            !completed && !selectedRunIds?.has(run.run_id) && 'ring-2 ring-inset ring-red-600 dark:ring-red-500',
                          )}
                          style={{ backgroundColor: completed ? getColorForRun(run, colorOverrides) : '#6b7280' }}
                          title={`${formatTimestamp(run.timestamp)} - ${completed ? formatDurationMinSec(runStats.duration) : run.status}`}
                        >
                          {completed && run.tests.tests_total - run.tests.tests_passed > 0 && (
                            <svg className="absolute inset-0 size-5" viewBox="0 0 20 20" fill="none">
                              <text x="10" y="15" textAnchor="middle" fill="white" fontSize="13" fontWeight="bold" fontFamily="system-ui">!</text>
                            </svg>
                          )}
                          {!completed && (
                            <svg className="absolute inset-0 size-5 text-red-600 dark:text-red-400" viewBox="0 0 20 20" fill="currentColor">
                              <path d="M4 4l12 12M4 16L16 4" stroke="currentColor" strokeWidth="2" fill="none" />
                            </svg>
                          )}
                        </button>
                      )
                    })}
                  </div>
                  <div className="hidden shrink-0 gap-3 border-l border-gray-200 pl-3 font-mono text-xs/5 text-gray-500 md:flex dark:border-gray-700 dark:text-gray-400">
                    <span className="w-10 text-center">{formatStatValue(stats.min)}</span>
                    <span className="w-10 text-center">{formatStatValue(stats.max)}</span>
                    <span className="w-10 text-center">{formatStatValue(stats.p95)}</span>
                    <span className="w-10 text-center">{formatStatValue(stats.p99)}</span>
                    <span className="w-10 text-center">{formatStatValue(stats.mean)}</span>
                    <span className="w-10 text-center">{formatStatValue(stats.last)}</span>
                  </div>
                </div>
              )
            })}
          </div>

          {/* Mobile stats summary */}
          <div className="mt-3 overflow-x-auto md:hidden">
            <table className="w-full text-xs/5">
              <thead>
                <tr className="text-gray-400 dark:text-gray-500">
                  <th className="py-1 pr-3 text-left font-medium">Client</th>
                  <th className="px-2 py-1 text-center font-medium">Min</th>
                  <th className="px-2 py-1 text-center font-medium">Max</th>
                  <th className="px-2 py-1 text-center font-medium">P95</th>
                  <th className="px-2 py-1 text-center font-medium">P99</th>
                  <th className="px-2 py-1 text-center font-medium">Mean</th>
                  <th className="px-2 py-1 text-center font-medium">Last</th>
                </tr>
              </thead>
              <tbody className="font-mono text-gray-500 dark:text-gray-400">
                {section.clients.map((client) => {
                  const s = metricMode === 'mgas' ? section.clientMgasStats[client] : section.clientDurationStats[client]
                  const fmt = (v?: number) => {
                    if (v === undefined) return '-'
                    if (metricMode === 'mgas') return v.toFixed(1)
                    return formatDurationCompact(v)
                  }
                  return (
                    <tr key={`${section.label}-${client}`} className="border-t border-gray-100 dark:border-gray-700">
                      <td className="py-1 pr-3">
                        <ClientBadge client={client} hideLabel />
                      </td>
                      <td className="px-2 py-1 text-center">{fmt(s.min)}</td>
                      <td className="px-2 py-1 text-center">{fmt(s.max)}</td>
                      <td className="px-2 py-1 text-center">{fmt(s.p95)}</td>
                      <td className="px-2 py-1 text-center">{fmt(s.p99)}</td>
                      <td className="px-2 py-1 text-center">{fmt(s.mean)}</td>
                      <td className="px-2 py-1 text-center">{fmt(s.last)}</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </div>
        </div>
      ))}

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
          <span className="relative mr-1 inline-block size-3 rounded-xs ring-2 ring-inset ring-orange-500" style={{ backgroundColor: COLORS[2] }}>
            <svg className="absolute inset-0 size-3" viewBox="0 0 12 12" fill="none">
              <text x="6" y="9.5" textAnchor="middle" fill="white" fontSize="9" fontWeight="bold" fontFamily="system-ui">!</text>
            </svg>
          </span>
          Has failures
        </span>
        <span className="flex items-center gap-1">
          <span className="relative inline-block size-3 rounded-xs bg-gray-500 ring-2 ring-inset ring-red-600">
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
                    <AlertTriangle className="size-3.5" />
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
                <div className="text-gray-500 dark:text-gray-400">
                  Instance ID: {tooltip.run.instance.id}
                </div>
                <div className="truncate text-gray-500 dark:text-gray-400" style={{ maxWidth: '200px' }}>
                  {tooltip.run.instance.image}
                </div>
                {tooltip.run.metadata && (() => {
                  const labels = Object.entries(tooltip.run.metadata!)
                    .filter(([k]) => !k.startsWith('github.') && k !== 'name')
                  if (labels.length === 0) return null
                  return (
                    <div className="flex flex-wrap gap-1">
                      {labels.map(([k, v]) => (
                        <span key={k} className="rounded-xs bg-gray-100 px-1.5 py-0.5 text-gray-600 dark:bg-gray-700 dark:text-gray-300">
                          {k}={v}
                        </span>
                      ))}
                    </div>
                  )
                })()}
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
                <div className="mt-1 text-gray-400 dark:text-gray-500">{selectable ? 'Click to select' : 'Click for details'}</div>
              </div>
            )
          })()}
        </div>
      )}
    </div>
  )
}
