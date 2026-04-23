import { useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { Check, Copy, GitCompareArrows, X } from 'lucide-react'
import clsx from 'clsx'
import type { RunResult } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { formatTimestamp } from '@/utils/date'
import { type GroupDef } from './groupUtils'
import { MAX_COMPARE_RUNS, MIN_COMPARE_RUNS } from './constants'

interface TestDetailModalProps {
  testName: string
  testOrder?: number
  groups: GroupDef[]
  /** All individual RunResult objects per group (same order as groups). */
  groupResults: RunResult[][]
  /** Timestamps per run per group for labeling. */
  groupTimestamps: number[][]
  /** Run IDs per group for linking to run detail pages. */
  groupRunIds: string[][]
  stepFilter: StepTypeOption[]
  sampleSize: number
  onClose: () => void
}

/**
 * TestDetailModal shows per-run MGas/s breakdown for a single test
 * across all groups. Helps identify outlier runs or variance within
 * a group that the averaged view hides.
 */
export function TestDetailModal({
  testName,
  testOrder,
  groups,
  groupResults,
  groupTimestamps,
  groupRunIds,
  stepFilter,
  sampleSize,
  onClose,
}: TestDetailModalProps) {
  const SLOT_COLORS = ['text-blue-700 dark:text-blue-300', 'text-orange-700 dark:text-orange-300', 'text-purple-700 dark:text-purple-300', 'text-green-700 dark:text-green-300', 'text-red-700 dark:text-red-300']

  const navigate = useNavigate()

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

      const runs = results.slice(0, sampleSize).map((result, ri) => {
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
          duration: stats?.time_total ?? 0,
        }
      })

      const mgasValues = runs.map((r) => r.mgas).filter((v): v is number => v !== undefined)
      const average = mgasValues.length > 0 ? mgasValues.reduce((a, b) => a + b, 0) / mgasValues.length : undefined
      const median = mgasValues.length > 0
        ? (() => {
            const sorted = [...mgasValues].sort((a, b) => a - b)
            const mid = Math.floor(sorted.length / 2)
            return sorted.length % 2 === 0 ? (sorted[mid - 1] + sorted[mid]) / 2 : sorted[mid]
          })()
        : undefined
      const min = mgasValues.length > 0 ? Math.min(...mgasValues) : undefined
      const max = mgasValues.length > 0 ? Math.max(...mgasValues) : undefined
      const stddev = mgasValues.length >= 2 && average !== undefined
        ? Math.sqrt(mgasValues.reduce((sum, v) => sum + (v - average) ** 2, 0) / (mgasValues.length - 1))
        : undefined

      const metaStr = Object.entries(group.metadata).map(([k, v]) => `${k}=${v}`).join(', ')
      const label = metaStr || group.client

      return { label, client: group.client, runs, average, median, min, max, stddev, mgasValues }
    })
  }, [groups, groupResults, groupTimestamps, groupRunIds, stepFilter, sampleSize, testName])

  // Find global min/max for the dot chart scaling.
  const allMgas = groupData.flatMap((g) => g.mgasValues)
  const globalMin = allMgas.length > 0 ? Math.min(...allMgas) : 0
  const globalMax = allMgas.length > 0 ? Math.max(...allMgas) : 1
  const range = globalMax - globalMin || 1

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50" onClick={onClose}>
      <div
        className="mx-4 flex max-h-[80vh] w-full max-w-3xl flex-col overflow-hidden rounded-sm bg-white shadow-xl dark:bg-gray-800"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="flex items-start justify-between gap-3 border-b border-gray-200 px-5 py-4 dark:border-gray-700">
          <div className="min-w-0">
            <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">
              {testOrder !== undefined ? `Test #${testOrder}` : 'Test Detail'}
            </h3>
            <div className="mt-0.5 flex items-start gap-1.5">
              <p className="break-all font-mono text-xs text-gray-500 dark:text-gray-400">
                {testName}
              </p>
              <CopyButton text={testName} />
            </div>
          </div>
          <button onClick={onClose} className="shrink-0 text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300">
            <X className="size-5" />
          </button>
        </div>

        {/* Body */}
        <div className="flex-1 overflow-y-auto px-5 py-4 pb-20">
          <div className="flex flex-col gap-6">
            {groupData.map((group, gi) => (
              <div key={gi} className="flex flex-col gap-2">
                {/* Group header */}
                <div className="flex items-center gap-2">
                  <img
                    src={`/img/clients/${group.client}.jpg`}
                    alt={group.client}
                    className="size-5 rounded-full object-cover"
                  />
                  <span className={clsx('text-sm/6 font-medium', SLOT_COLORS[gi % SLOT_COLORS.length])}>
                    {group.label}
                  </span>
                </div>

                {/* Dot chart — each dot is one run's MGas/s for this test */}
                <div className="relative h-6 rounded-xs bg-gray-100 dark:bg-gray-700">
                  {group.mgasValues.map((v, i) => {
                    const pct = ((v - globalMin) / range) * 100
                    return (
                      <span
                        key={i}
                        className="absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full bg-current opacity-70"
                        style={{ left: `${Math.max(2, Math.min(98, pct))}%`, color: SLOT_COLORS[gi % SLOT_COLORS.length].includes('blue') ? '#3b82f6' : SLOT_COLORS[gi % SLOT_COLORS.length].includes('orange') ? '#f97316' : SLOT_COLORS[gi % SLOT_COLORS.length].includes('purple') ? '#a855f7' : SLOT_COLORS[gi % SLOT_COLORS.length].includes('green') ? '#22c55e' : '#ef4444' }}
                        title={`${v.toFixed(2)} MGas/s`}
                      />
                    )
                  })}
                  {/* Min/Max labels */}
                  <span className="absolute left-1 top-full mt-0.5 text-xs text-gray-400">{globalMin.toFixed(1)}</span>
                  <span className="absolute right-1 top-full mt-0.5 text-xs text-gray-400">{globalMax.toFixed(1)}</span>
                </div>

                {/* Stats summary */}
                {group.min !== undefined && (
                  <div className="mt-4 grid grid-cols-7 gap-x-2 border-t border-gray-200 pt-2 text-xs dark:border-gray-700">
                    <StatCell
                      label="Avg"
                      value={group.average?.toFixed(2)}
                      title="Arithmetic mean of MGas/s across the sampled runs in this group."
                    />
                    <StatCell
                      label="Median"
                      value={group.median?.toFixed(2)}
                      title="Middle value of MGas/s across the sampled runs. Less sensitive to outliers than the average."
                    />
                    <StatCell
                      label="Min"
                      value={group.min.toFixed(2)}
                      title="Lowest MGas/s observed across the sampled runs."
                    />
                    <StatCell
                      label="Max"
                      value={group.max?.toFixed(2)}
                      title="Highest MGas/s observed across the sampled runs."
                    />
                    <StatCell
                      label="Range"
                      value={((group.max ?? 0) - (group.min ?? 0)).toFixed(2)}
                      title="Max minus min — the spread of MGas/s across the sampled runs."
                    />
                    <StatCell
                      label="σ"
                      value={group.stddev?.toFixed(2)}
                      title="Sample standard deviation of MGas/s. How much individual runs typically deviate from the average."
                    />
                    <StatCell
                      label="CV"
                      value={group.stddev !== undefined && group.average !== undefined && group.average > 0
                        ? `${((group.stddev / group.average) * 100).toFixed(1)}%`
                        : undefined}
                      title="Coefficient of Variation — standard deviation as a percentage of the average. Lower = more consistent across runs."
                    />
                  </div>
                )}

                {/* Per-run table (collapsed by default) */}
                <button
                  type="button"
                  onClick={() => toggleGroupExpand(gi)}
                  className="flex items-center gap-1 text-xs font-medium text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
                >
                  <span className={clsx('transition-transform', expandedGroups.has(gi) && 'rotate-90')}>▶</span>
                  {expandedGroups.has(gi) ? 'Hide' : 'Show'} individual runs ({group.runs.length})
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

function StatCell({ label, value, title }: { label: string; value?: string; title?: string }) {
  return (
    <div className="flex flex-col" title={title}>
      <span className="text-[10px] uppercase tracking-wide text-gray-400 dark:text-gray-500">{label}</span>
      <span className="font-mono tabular-nums text-gray-700 dark:text-gray-200">{value ?? '—'}</span>
    </div>
  )
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  return (
    <button
      type="button"
      onClick={(e) => {
        e.stopPropagation()
        navigator.clipboard.writeText(text)
        setCopied(true)
        setTimeout(() => setCopied(false), 1500)
      }}
      className="shrink-0 text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300"
      title="Copy test name"
    >
      {copied ? <Check className="size-3.5 text-green-500" /> : <Copy className="size-3.5" />}
    </button>
  )
}
