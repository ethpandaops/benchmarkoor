import { useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { GitCompareArrows, X } from 'lucide-react'
import clsx from 'clsx'
import type { RunResult, SuiteTest } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { TestName } from '@/components/shared/TestName'
import { EESTInfoContent, type OpcodeSortMode } from '@/components/suite-detail/TestFilesList'
import { formatTimestamp } from '@/utils/date'
import { type GroupDef } from './groupUtils'
import { MAX_COMPARE_RUNS, MIN_COMPARE_RUNS } from './constants'
import { type HeatmapColorModel, baselineRatio, formatRatio, heatmapColor } from './heatmapColor'
import { formatDuration } from '@/utils/format'
import { SLOW_COLOR, isSlowPayload } from '@/utils/perfThreshold'

interface TestDetailModalProps {
  testName: string
  testOrder?: number
  /** The suite entry of the test, for its EEST info and opcode counts. */
  suiteTest?: SuiteTest
  groups: GroupDef[]
  /** All individual RunResult objects per group (same order as groups). */
  groupResults: RunResult[][]
  /** Timestamps per run per group for labeling. */
  groupTimestamps: number[][]
  /** Run IDs per group for linking to run detail pages. */
  groupRunIds: string[][]
  /** Averaged MGas/s per group (same order as groups) — the value the heatmap tile shows. */
  groupMgas: (number | undefined)[]
  /** Averaged total engine_newPayload time per group in nanoseconds (same order as groups). */
  groupDurations: (number | undefined)[]
  /** Group index of the baseline, or -1 when the baseline group has no result. */
  baselineGroupIdx: number
  /** Colour model of the heatmap, so the cards match its tiles. */
  heatmapModel: HeatmapColorModel
  stepFilter: StepTypeOption[]
  /** Current page-level search query (used to highlight active chips). */
  searchQuery?: string
  /** Toggle a `key:value` term in the page-level search. */
  onChipFilterToggle?: (term: string) => void
  onClose: () => void
}

// Text colour per slot, matching the group card colours of the builder,
// and the dot fill in the 500 shade of the same hues.
const SLOT_TEXT_COLORS = ['text-blue-700 dark:text-blue-300', 'text-orange-700 dark:text-orange-300', 'text-purple-700 dark:text-purple-300', 'text-green-700 dark:text-green-300', 'text-red-700 dark:text-red-300']
const DOT_COLORS = ['#3b82f6', '#f97316', '#a855f7', '#22c55e', '#ef4444']

/**
 * TestDetailModal shows per-run MGas/s breakdown for a single test
 * across all groups. Helps identify outlier runs or variance within
 * a group that the averaged view hides.
 */
export function TestDetailModal({
  testName,
  testOrder,
  suiteTest,
  groups,
  groupResults,
  groupTimestamps,
  groupRunIds,
  groupMgas,
  groupDurations,
  baselineGroupIdx,
  heatmapModel,
  stepFilter,
  searchQuery,
  onChipFilterToggle,
  onClose,
}: TestDetailModalProps) {

  const navigate = useNavigate()
  const [opcodeSort, setOpcodeSort] = useState<OpcodeSortMode>('name')

  const [expandedGroups, setExpandedGroups] = useState<Set<number>>(new Set())
  const toggleGroupExpand = (gi: number) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev)
      if (next.has(gi)) next.delete(gi); else next.add(gi)
      return next
    })
  }

  const [selectedRunIds, setSelectedRunIds] = useState<Set<string>>(new Set())
  const toggleRunSelected = (runId: string) => {
    setSelectedRunIds((prev) => {
      const next = new Set(prev)
      if (next.has(runId)) {
        next.delete(runId)
      } else {
        if (next.size >= MAX_COMPARE_RUNS) return prev
        next.add(runId)
      }
      return next
    })
  }
  const clearSelection = () => setSelectedRunIds(new Set())
  const handleCompare = () => {
    if (selectedRunIds.size < MIN_COMPARE_RUNS) return
    navigate({ to: '/compare', search: { runs: Array.from(selectedRunIds).join(',') } })
  }

  type SortKey = 'run' | 'mgas' | 'gasUsed' | 'duration'
  const [sortKey, setSortKey] = useState<SortKey>('run')
  const [sortDir, setSortDir] = useState<'asc' | 'desc'>('asc')

  const toggleSort = (key: SortKey) => {
    if (sortKey === key) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortKey(key)
      setSortDir(key === 'mgas' ? 'desc' : 'asc')
    }
  }

  const groupData = useMemo(() => {
    return groups.map((group, gi) => {
      const results = groupResults[gi] ?? []
      const timestamps = groupTimestamps[gi] ?? []
      const runIds = groupRunIds[gi] ?? []

      const runs = results.map((result, ri) => {
        const entry = result.tests[testName]
        const stats = entry ? getAggregatedStats(entry, stepFilter) : undefined
        const mgas = stats && stats.gas_used_time_total > 0
          ? (stats.gas_used_total * 1000) / stats.gas_used_time_total
          : undefined

        return {
          runId: runIds[ri],
          timestamp: timestamps[ri],
          mgas,
          gasUsed: stats?.gas_used_total ?? 0,
          gasUsedTime: stats?.gas_used_time_total ?? 0,
          duration: stats?.time_total ?? 0,
        }
      })

      const mgasValues = runs.map((r) => r.mgas).filter((v): v is number => v !== undefined)
      // The average throughput is total gas over total time, the same
      // number the averaged group result and the heatmap use. Time is the
      // measured quantity, so a slow run weighs as much as it lasted. The
      // arithmetic mean of the per-run rates reads high whenever the runs
      // spread; it is kept only for σ and CV, which are defined around it.
      const measured = runs.filter((r) => r.mgas !== undefined)
      const totalGasTime = measured.reduce((sum, r) => sum + r.gasUsedTime, 0)
      const average = totalGasTime > 0 ? (measured.reduce((sum, r) => sum + r.gasUsed, 0) * 1000) / totalGasTime : undefined
      const mean = mgasValues.length > 0 ? mgasValues.reduce((a, b) => a + b, 0) / mgasValues.length : undefined
      const median = mgasValues.length > 0
        ? (() => {
            const sorted = [...mgasValues].sort((a, b) => a - b)
            const mid = Math.floor(sorted.length / 2)
            return sorted.length % 2 === 0 ? (sorted[mid - 1] + sorted[mid]) / 2 : sorted[mid]
          })()
        : undefined
      const min = mgasValues.length > 0 ? Math.min(...mgasValues) : undefined
      const max = mgasValues.length > 0 ? Math.max(...mgasValues) : undefined
      const stddev = mgasValues.length >= 2 && mean !== undefined
        ? Math.sqrt(mgasValues.reduce((sum, v) => sum + (v - mean) ** 2, 0) / (mgasValues.length - 1))
        : undefined

      const metaStr = Object.entries(group.metadata).map(([k, v]) => `${k}=${v}`).join(', ')
      const label = metaStr || group.client

      return { label, client: group.client, runs, average, mean, median, min, max, stddev, mgasValues }
    })
  }, [groups, groupResults, groupTimestamps, groupRunIds, stepFilter, testName])

  // Find global min/max for the dot chart scaling.
  const allMgas = groupData.flatMap((g) => g.mgasValues)
  const globalMin = allMgas.length > 0 ? Math.min(...allMgas) : 0
  const globalMax = allMgas.length > 0 ? Math.max(...allMgas) : 1
  const range = globalMax - globalMin || 1
  const dotLeft = (v: number) => `${Math.max(2, Math.min(98, ((v - globalMin) / range) * 100))}%`

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div
        className="mx-4 flex max-h-[90vh] w-full max-w-6xl flex-col overflow-hidden rounded-sm bg-white shadow-xl dark:bg-gray-800"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-start justify-between gap-3 border-b border-gray-200 px-5 py-4 dark:border-gray-700">
          <div className="min-w-0">
            <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">
              {testOrder !== undefined ? `Test #${testOrder}` : 'Test Detail'}
            </h3>
            <div className="mt-0.5 text-xs text-gray-500 dark:text-gray-400">
              <TestName name={testName} showRawBelow showCopy onChipClick={onChipFilterToggle} activeQuery={searchQuery} />
            </div>
          </div>
          <button onClick={onClose} className="shrink-0 text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300">
            <X className="size-5" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto px-5 py-4 pb-20">
          <div className="flex flex-col gap-6">
            {suiteTest && (
              <EESTInfoContent test={suiteTest} opcodeSort={opcodeSort} onOpcodeSortChange={setOpcodeSort} />
            )}
            {/* One card per group with the averaged value, tinted like its heatmap tile */}
            <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
              {groupData.map((group, gi) => {
                const { metric, mode, threshold, slowMs } = heatmapModel
                const values = metric === 'mgas' ? groupMgas : groupDurations
                const value = values[gi]
                const base = baselineGroupIdx >= 0 ? values[baselineGroupIdx] : undefined
                const color = heatmapColor(value, base, heatmapModel)
                const isBaseline = mode === 'baseline' && gi === baselineGroupIdx && groupData.length >= 2
                // The other metric, in small print under the main value.
                const other = metric === 'mgas' ? groupDurations[gi] : groupMgas[gi]
                const duration = groupDurations[gi]
                const slow = duration !== undefined && isSlowPayload(duration, slowMs)
                const note = isBaseline
                  ? 'baseline'
                  : value === undefined
                    ? '\u00a0'
                    : mode === 'baseline'
                      ? base !== undefined && base > 0 && value > 0 ? `${formatRatio(baselineRatio(value, base, metric))} vs baseline` : '\u00a0'
                      : metric === 'mgas'
                        ? `${formatRatio(value / threshold)} vs ${threshold} MGas/s`
                        : `${formatRatio((slowMs * 1_000_000) / value)} vs ${formatDuration(slowMs * 1_000_000)} limit`
                return (
                  <div
                    key={gi}
                    className="flex flex-col gap-1 rounded-sm border-l-4 border-gray-300 bg-gray-100 px-3 py-2 dark:border-gray-600 dark:bg-gray-700/50"
                    style={{
                      ...(color ? { borderColor: color, backgroundColor: `${color}26` } : {}),
                      // Same slow-payload outline as the heatmap tile.
                      ...(slow ? { outline: `2px solid ${SLOW_COLOR}`, outlineOffset: '-2px' } : {}),
                    }}
                  >
                    <span className={clsx('inline-flex items-center gap-1.5 text-xs/5 font-medium', SLOT_TEXT_COLORS[gi % SLOT_TEXT_COLORS.length])}>
                      <img src={`/img/clients/${group.client}.jpg`} alt={group.client} className="size-3.5 rounded-full object-cover" />
                      <span className="truncate">{group.label}</span>
                      {slow && (
                        <span className="rounded-xs px-1 text-[10px]/4 font-medium" style={{ backgroundColor: `${SLOW_COLOR}26`, color: SLOW_COLOR }} title={`Payload time above ${formatDuration(slowMs * 1_000_000)}`}>
                          slow
                        </span>
                      )}
                    </span>
                    <span className="font-mono text-lg/7 font-semibold text-gray-900 dark:text-gray-100">
                      {value === undefined ? '—' : metric === 'mgas' ? value.toFixed(2) : formatDuration(value)}
                      {metric === 'mgas' && <span className="ml-1 text-xs/5 font-normal text-gray-500 dark:text-gray-400">MGas/s</span>}
                    </span>
                    <span className="text-xs/5 text-gray-500 dark:text-gray-400">{note}</span>
                    <span className="font-mono text-xs/5 text-gray-400 dark:text-gray-500">
                      {other === undefined ? '\u00a0' : metric === 'mgas' ? formatDuration(other) : `${other.toFixed(2)} MGas/s`}
                    </span>
                  </div>
                )
              })}
            </div>

            {/* Strips: all groups on one axis, then one strip per group right
                under it, so the overlap between groups is visible at a glance */}
            <div className="flex flex-col gap-1">
              {groupData.length >= 2 && (
                <StripRow label={<span className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">All groups</span>}>
                  {groupData.map((group, gi) =>
                    group.mgasValues.map((v, i) => (
                      <span
                        key={`${gi}-${i}`}
                        className="absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full opacity-70"
                        style={{ left: dotLeft(v), backgroundColor: DOT_COLORS[gi % DOT_COLORS.length] }}
                        title={`${group.label}: ${v.toFixed(2)} MGas/s`}
                      />
                    )),
                  )}
                </StripRow>
              )}
              {groupData.map((group, gi) => (
                <StripRow key={gi} label={<GroupLabel group={group} gi={gi} />}>
                  {group.mgasValues.map((v, i) => (
                    <span
                      key={i}
                      className="absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full opacity-70"
                      style={{ left: dotLeft(v), backgroundColor: DOT_COLORS[gi % DOT_COLORS.length] }}
                      title={`${v.toFixed(2)} MGas/s`}
                    />
                  ))}
                </StripRow>
              ))}
              {/* One shared axis under the strips */}
              <div className="flex text-xs text-gray-400">
                <span className="w-40 shrink-0" />
                <span className="flex flex-1 justify-between px-1">
                  <span>{globalMin.toFixed(1)}</span>
                  <span>{globalMax.toFixed(1)}</span>
                </span>
              </div>
            </div>

            {/* One table with the stats of every group */}
            <table className="w-full text-xs">
              <thead>
                <tr className="border-b border-gray-200 text-left text-[10px] uppercase tracking-wide text-gray-400 dark:border-gray-700 dark:text-gray-500">
                  <th className="py-1 pr-2 font-medium">Group</th>
                  <th className="px-2 py-1 text-right font-medium" title="Total gas over total time of the sampled runs — the throughput of the group as a whole, and the value the heatmap and the charts use. A slow run weighs as much as it lasted.">Avg</th>
                  <th className="px-2 py-1 text-right font-medium" title="Middle value of MGas/s across the sampled runs. Less sensitive to outliers than the average.">Median</th>
                  <th className="px-2 py-1 text-right font-medium" title="Lowest MGas/s observed across the sampled runs.">Min</th>
                  <th className="px-2 py-1 text-right font-medium" title="Highest MGas/s observed across the sampled runs.">Max</th>
                  <th className="px-2 py-1 text-right font-medium" title="Max minus min — the spread of MGas/s across the sampled runs.">Range</th>
                  <th className="px-2 py-1 text-right font-medium" title="Sample standard deviation of the per-run MGas/s around their arithmetic mean. How much individual runs typically deviate.">σ</th>
                  <th className="px-2 py-1 text-right font-medium" title="Coefficient of Variation — standard deviation as a percentage of the arithmetic mean of the per-run MGas/s. Lower = more consistent across runs.">CV</th>
                  <th className="px-2 py-1 text-right font-medium">Runs</th>
                </tr>
              </thead>
              <tbody className="font-mono tabular-nums text-gray-700 dark:text-gray-200">
                {groupData.map((group, gi) => (
                  <tr key={gi} className="border-b border-gray-100 last:border-0 dark:border-gray-700/50">
                    <td className="py-1 pr-2 font-sans"><GroupLabel group={group} gi={gi} /></td>
                    <td className="px-2 py-1 text-right">{group.average?.toFixed(2) ?? '—'}</td>
                    <td className="px-2 py-1 text-right">{group.median?.toFixed(2) ?? '—'}</td>
                    <td className="px-2 py-1 text-right">{group.min?.toFixed(2) ?? '—'}</td>
                    <td className="px-2 py-1 text-right">{group.max?.toFixed(2) ?? '—'}</td>
                    <td className="px-2 py-1 text-right">{group.min !== undefined && group.max !== undefined ? (group.max - group.min).toFixed(2) : '—'}</td>
                    <td className="px-2 py-1 text-right">{group.stddev?.toFixed(2) ?? '—'}</td>
                    <td className="px-2 py-1 text-right">
                      {group.stddev !== undefined && group.mean !== undefined && group.mean > 0
                        ? `${((group.stddev / group.mean) * 100).toFixed(1)}%`
                        : '—'}
                    </td>
                    <td className="px-2 py-1 text-right">{group.mgasValues.length}</td>
                  </tr>
                ))}
              </tbody>
            </table>

            {/* Per-run tables, one per group, collapsed by default */}
            <div className="flex flex-col gap-2">
              {groupData.map((group, gi) => (
                <div key={gi} className="flex flex-col gap-1">
                  <button
                    type="button"
                    onClick={() => toggleGroupExpand(gi)}
                    className="flex items-center gap-2 text-xs font-medium text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                  >
                    <span className={clsx('transition-transform', expandedGroups.has(gi) && 'rotate-90')}>▶</span>
                    <GroupLabel group={group} gi={gi} />
                    <span>{expandedGroups.has(gi) ? 'Hide' : 'Show'} individual runs ({group.runs.length})</span>
                  </button>
                  {expandedGroups.has(gi) && <table className="w-full text-xs">
                    <thead>
                      <tr className="border-b border-gray-200 text-gray-500 dark:border-gray-700 dark:text-gray-400">
                        <th className="w-6 px-2 py-1"></th>
                        <SortableHeader label="Run" sortKey="run" currentKey={sortKey} currentDir={sortDir} onSort={toggleSort} align="left" />
                        <SortableHeader label="MGas/s" sortKey="mgas" currentKey={sortKey} currentDir={sortDir} onSort={toggleSort} align="right" />
                        <SortableHeader label="Gas Used" sortKey="gasUsed" currentKey={sortKey} currentDir={sortDir} onSort={toggleSort} align="right" />
                        <SortableHeader label="Duration" sortKey="duration" currentKey={sortKey} currentDir={sortDir} onSort={toggleSort} align="right" />
                      </tr>
                    </thead>
                    <tbody className="text-gray-700 dark:text-gray-200">
                      {[...group.runs].sort((a, b) => {
                        let cmp = 0
                        switch (sortKey) {
                          case 'run': cmp = (a.timestamp ?? 0) - (b.timestamp ?? 0); break
                          case 'mgas': cmp = (a.mgas ?? 0) - (b.mgas ?? 0); break
                          case 'gasUsed': cmp = a.gasUsed - b.gasUsed; break
                          case 'duration': cmp = a.duration - b.duration; break
                        }
                        return sortDir === 'asc' ? cmp : -cmp
                      }).map((run, ri) => {
                        const isSelected = !!run.runId && selectedRunIds.has(run.runId)
                        const selectable = !!run.runId
                        const atCap = selectedRunIds.size >= MAX_COMPARE_RUNS && !isSelected
                        return (
                          <tr
                            key={ri}
                            className="cursor-pointer border-b border-gray-100 last:border-0 hover:bg-gray-50 dark:border-gray-700/50 dark:hover:bg-gray-700/50"
                            onClick={() => { if (run.runId) window.open(`/runs/${run.runId}?testModal=${encodeURIComponent(testName)}`, '_blank') }}
                          >
                            <td className="px-2 py-1" onClick={(e) => e.stopPropagation()}>
                              <input
                                type="checkbox"
                                checked={isSelected}
                                disabled={!selectable || atCap}
                                onChange={() => run.runId && toggleRunSelected(run.runId)}
                                title={atCap ? `Maximum ${MAX_COMPARE_RUNS} runs can be compared` : 'Select for comparison'}
                                className="size-3.5 cursor-pointer accent-blue-600 disabled:cursor-not-allowed disabled:opacity-40 dark:accent-blue-500"
                              />
                            </td>
                            <td className="px-2 py-1">{run.timestamp ? formatTimestamp(run.timestamp) : `Run ${ri + 1}`}</td>
                            <td className="px-2 py-1 text-right font-mono">
                              {run.mgas !== undefined ? run.mgas.toFixed(2) : '-'}
                            </td>
                            <td className="px-2 py-1 text-right font-mono">
                              {run.gasUsed > 0 ? `${(run.gasUsed / 1_000_000).toFixed(1)}M` : '-'}
                            </td>
                            <td className="px-2 py-1 text-right font-mono">
                              {run.duration > 0 ? `${(run.duration / 1_000_000_000).toFixed(2)}s` : '-'}
                            </td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>}
                </div>
              ))}
            </div>
          </div>
        </div>

        {/* Footer — comparison action bar */}
        {selectedRunIds.size > 0 && (
          <div className="flex items-center justify-between gap-3 border-t border-gray-200 bg-gray-50 px-5 py-3 dark:border-gray-700 dark:bg-gray-700/40">
            <span className="text-xs/5 text-gray-600 dark:text-gray-300">
              {selectedRunIds.size} of {MAX_COMPARE_RUNS} selected
              {selectedRunIds.size < MIN_COMPARE_RUNS && (
                <span className="ml-1 text-gray-400 dark:text-gray-500">
                  · select at least {MIN_COMPARE_RUNS} to compare
                </span>
              )}
            </span>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={clearSelection}
                className="rounded-xs px-2 py-1 text-xs/5 font-medium text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-600 dark:hover:text-gray-200"
              >
                Clear
              </button>
              <button
                type="button"
                onClick={handleCompare}
                disabled={selectedRunIds.size < MIN_COMPARE_RUNS}
                className="inline-flex items-center gap-1.5 rounded-xs bg-blue-600 px-2.5 py-1 text-xs/5 font-medium text-white hover:bg-blue-500 disabled:cursor-not-allowed disabled:bg-gray-300 disabled:text-gray-500 dark:disabled:bg-gray-600 dark:disabled:text-gray-400"
              >
                <GitCompareArrows className="size-3.5" />
                Compare {selectedRunIds.size} run{selectedRunIds.size === 1 ? '' : 's'}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}

function SortableHeader({ label, sortKey, currentKey, currentDir, onSort, align }: {
  label: string
  sortKey: string
  currentKey: string
  currentDir: 'asc' | 'desc'
  onSort: (key: never) => void
  align: 'left' | 'right'
}) {
  const active = currentKey === sortKey
  const arrow = active ? (currentDir === 'asc' ? ' ▲' : ' ▼') : ''

  return (
    <th
      className={clsx(
        'cursor-pointer select-none px-2 py-1 font-medium',
        align === 'right' ? 'text-right' : 'text-left',
        active && 'text-gray-900 dark:text-gray-100',
      )}
      onClick={() => onSort(sortKey as never)}
    >
      {label}{arrow}
    </th>
  )
}

function GroupLabel({ group, gi }: { group: { client: string; label: string }; gi: number }) {
  return (
    <span className={clsx('inline-flex min-w-0 items-center gap-1.5 text-xs/5 font-medium', SLOT_TEXT_COLORS[gi % SLOT_TEXT_COLORS.length])}>
      <img src={`/img/clients/${group.client}.jpg`} alt={group.client} className="size-4 shrink-0 rounded-full object-cover" />
      <span className="truncate">{group.label}</span>
    </span>
  )
}

// StripRow is one labelled dot strip. The label column has a fixed width
// so the strips of every row share the same axis.
function StripRow({ label, children }: { label: React.ReactNode; children: React.ReactNode }) {
  return (
    <div className="flex items-center gap-2">
      <div className="w-40 shrink-0 truncate">{label}</div>
      <div className="relative h-5 flex-1 rounded-xs bg-gray-100 dark:bg-gray-700">{children}</div>
    </div>
  )
}
