import { useMemo, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { GitCompareArrows, Medal, X } from 'lucide-react'
import clsx from 'clsx'
import type { RunResult, SuiteTest } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { TestName } from '@/components/shared/TestName'
import { EESTInfoContent, PayloadSizesContent, type OpcodeSortMode } from '@/components/suite-detail/TestFilesList'
import { ExecutionsList } from '@/components/run-detail/ExecutionsList'
import { type StepType } from '@/api/hooks/useTestDetails'
import { formatTimestamp } from '@/utils/date'
import { type GroupDef } from './groupUtils'
import { MAX_COMPARE_RUNS, MEDAL_CLASSES, MIN_COMPARE_RUNS } from './constants'
import { type HeatmapColorModel, type HeatmapMetric, baselineRatio, formatRatio, heatmapColor } from './heatmapColor'
import { formatBytes, formatDuration } from '@/utils/format'
import { SLOW_COLOR, isSlowPayload } from '@/utils/perfThreshold'

interface TestDetailModalProps {
  testName: string
  testOrder?: number
  /** The suite entry of the test, for its EEST info and opcode counts. */
  suiteTest?: SuiteTest
  /** Suite the groups share. The step payloads come from its request files. */
  suiteHash?: string
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

interface MetricSummary {
  /** Per-run values, in run order. */
  values: number[]
  /** The group value: the same number the averaged group result and the heatmap use. */
  average?: number
  /** Arithmetic mean of the per-run values; σ and CV are defined around it. */
  mean?: number
  median?: number
  min?: number
  max?: number
  stddev?: number
}

function summarize(values: number[], average: number | undefined): MetricSummary {
  if (values.length === 0) return { values, average }
  const mean = values.reduce((a, b) => a + b, 0) / values.length
  const sorted = [...values].sort((a, b) => a - b)
  const mid = Math.floor(sorted.length / 2)
  const median = sorted.length % 2 === 0 ? (sorted[mid - 1] + sorted[mid]) / 2 : sorted[mid]
  const stddev = values.length >= 2
    ? Math.sqrt(values.reduce((sum, v) => sum + (v - mean) ** 2, 0) / (values.length - 1))
    : undefined
  return { values, average, mean, median, min: sorted[0], max: sorted[sorted.length - 1], stddev }
}

/**
 * TestDetailModal shows per-run MGas/s breakdown for a single test
 * across all groups. Helps identify outlier runs or variance within
 * a group that the averaged view hides.
 */
export function TestDetailModal({
  testName,
  testOrder,
  suiteTest,
  suiteHash,
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

  type SortKey = 'run' | 'mgas' | 'gasUsed' | 'payload' | 'duration'
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

      // The measured quantity is the payload time; MGas/s is derived from
      // it. The group's average throughput is total gas over total time,
      // the same number the averaged group result and the heatmap use: a
      // slow run weighs as much as it lasted. The arithmetic mean of the
      // per-run rates reads high whenever the runs spread; it stays only
      // behind σ and CV, which are defined around it. For the duration the
      // plain mean is that same number.
      const measured = runs.filter((r) => r.mgas !== undefined)
      const totalGasTime = measured.reduce((sum, r) => sum + r.gasUsedTime, 0)
      const mgasAverage = totalGasTime > 0 ? (measured.reduce((sum, r) => sum + r.gasUsed, 0) * 1000) / totalGasTime : undefined
      const mgas = summarize(measured.map((r) => r.mgas as number), mgasAverage)
      const payloadTimes = measured.map((r) => r.gasUsedTime)
      const duration = summarize(payloadTimes, measured.length > 0 ? totalGasTime / measured.length : undefined)

      const metaStr = Object.entries(group.metadata).map(([k, v]) => `${k}=${v}`).join(', ')
      const label = metaStr || group.client

      return { label, client: group.client, runs, metrics: { mgas, duration } }
    })
  }, [groups, groupResults, groupTimestamps, groupRunIds, stepFilter, testName])


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
            <MetricSection metric="mgas" title="MGas/s" groupData={groupData} values={groupMgas} durations={groupDurations} baselineGroupIdx={baselineGroupIdx} heatmapModel={heatmapModel} />
            <MetricSection metric="duration" title="Payload time" groupData={groupData} values={groupDurations} durations={groupDurations} baselineGroupIdx={baselineGroupIdx} heatmapModel={heatmapModel} />

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
                        <SortableHeader label="Payload time" sortKey="payload" currentKey={sortKey} currentDir={sortDir} onSort={toggleSort} align="right" />
                        <SortableHeader label="Total time" sortKey="duration" currentKey={sortKey} currentDir={sortDir} onSort={toggleSort} align="right" />
                      </tr>
                    </thead>
                    <tbody className="text-gray-700 dark:text-gray-200">
                      {[...group.runs].sort((a, b) => {
                        let cmp = 0
                        switch (sortKey) {
                          case 'run': cmp = (a.timestamp ?? 0) - (b.timestamp ?? 0); break
                          case 'mgas': cmp = (a.mgas ?? 0) - (b.mgas ?? 0); break
                          case 'gasUsed': cmp = a.gasUsed - b.gasUsed; break
                          case 'payload': cmp = a.gasUsedTime - b.gasUsedTime; break
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
                              {run.gasUsedTime > 0 ? formatDuration(run.gasUsedTime) : '-'}
                            </td>
                            <td className="px-2 py-1 text-right font-mono" title="Wall time of the whole test step, payloads and other calls">
                              {run.duration > 0 ? formatDuration(run.duration) : '-'}
                            </td>
                          </tr>
                        )
                      })}
                    </tbody>
                  </table>}
                </div>
              ))}
            </div>

            {suiteHash && suiteTest && <StepPayloads suiteHash={suiteHash} test={suiteTest} />}
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

/**
 * Step payloads of the test, in the layout of the run detail modal: one
 * tab per step, one row per call, each row expands into the JSON. The
 * suite holds the request files, so the list is the same for every group.
 * The responses, the timings and the statuses belong to a run and stay on
 * the run detail page.
 */
function StepPayloads({ suiteHash, test }: { suiteHash: string; test: SuiteTest }) {
  const steps = ([
    { key: 'test', label: 'Test', file: test.test },
    { key: 'setup', label: 'Setup', file: test.setup },
    { key: 'cleanup', label: 'Cleanup', file: test.cleanup },
  ] as const).filter((s) => !!s.file)
  const [activeKey, setActiveKey] = useState<StepType | null>(null)
  const [showSizes, setShowSizes] = useState(false)
  if (steps.length === 0) return null
  const active = steps.find((s) => s.key === activeKey) ?? steps[0]
  const sizes = test.payload_sizes?.[active.key]
  const sszTotal = sizes?.ssz_full.reduce((sum, n) => sum + n, 0) ?? 0

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-baseline gap-2">
        <h4 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Payloads</h4>
        <span className="text-xs/5 text-gray-500 dark:text-gray-400">the requests of the suite, shared by every group</span>
      </div>
      <div className="flex gap-1 border-b border-gray-200 dark:border-gray-700">
        {steps.map(({ key, label }) => {
          const counts = test.tx_counts?.[key] ?? []
          const payloads = counts.length
          const txs = counts.reduce((sum, n) => sum + n, 0)
          return (
            <button
              key={key}
              type="button"
              onClick={() => setActiveKey(key)}
              className={clsx(
                'flex items-center gap-2 border-b-2 px-4 py-2 text-sm font-medium transition-colors',
                active.key === key
                  ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                  : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300',
              )}
            >
              {label}
              {payloads > 0 && (
                <span
                  className="rounded-full bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-600 dark:bg-gray-700 dark:text-gray-300"
                  title={`${payloads} engine_newPayload call${payloads === 1 ? '' : 's'}`}
                >
                  {payloads}
                </span>
              )}
              {txs > 0 && (
                <span
                  className="rounded-xs bg-violet-100 px-1.5 py-0.5 text-xs font-medium text-violet-700 dark:bg-violet-900/40 dark:text-violet-300"
                  title={`${txs.toLocaleString()} transaction${txs === 1 ? '' : 's'} over the ${payloads} engine_newPayload call${payloads === 1 ? '' : 's'} of the ${label.toLowerCase()} step`}
                >
                  {txs.toLocaleString()} tx{txs === 1 ? '' : 's'}
                </span>
              )}
            </button>
          )
        })}
      </div>
      {sszTotal > 0 && (
        <div className="flex flex-col gap-2">
          <button
            type="button"
            onClick={() => setShowSizes((v) => !v)}
            className="flex items-center gap-2 self-start text-xs font-medium text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200"
          >
            <span className={clsx('transition-transform', showSizes && 'rotate-90')}>▶</span>
            {showSizes ? 'Hide' : 'Show'} payload size breakdown ({formatBytes(sszTotal)} SSZ in the {active.label.toLowerCase()} step)
          </button>
          {showSizes && <PayloadSizesContent test={test} only={active.key} hideTitle />}
        </div>
      )}
      <ExecutionsList
        key={active.key}
        suiteHash={suiteHash}
        testName={test.name}
        stepType={active.key}
        txCounts={test.tx_counts?.[active.key]}
        payloadSizes={sizes}
      />
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

interface GroupSummary {
  label: string
  client: string
  metrics: Record<HeatmapMetric, MetricSummary>
}

// MetricSection is the cards, the dot strips and the stats table of one
// metric. The cards colour with the section's metric and the heatmap's
// mode and limits.
function MetricSection({ metric, title, groupData, values, durations, baselineGroupIdx, heatmapModel }: {
  metric: HeatmapMetric
  title: string
  groupData: GroupSummary[]
  /** Averaged value of this metric per group — the value the heatmap tile shows. */
  values: (number | undefined)[]
  /** Averaged payload time per group, for the slow marker. */
  durations: (number | undefined)[]
  baselineGroupIdx: number
  heatmapModel: HeatmapColorModel
}) {
  const model = { ...heatmapModel, metric }
  const { mode, threshold, slowMs } = model
  const fmt = (v: number) => (metric === 'mgas' ? v.toFixed(2) : formatDuration(v))
  const fmtAxis = (v: number) => (metric === 'mgas' ? v.toFixed(1) : formatDuration(v))
  const unit = metric === 'mgas' ? 'MGas/s' : 'payload time'

  // Global min/max of the metric for the dot chart scaling.
  const allValues = groupData.flatMap((g) => g.metrics[metric].values)
  const globalMin = allValues.length > 0 ? Math.min(...allValues) : 0
  const globalMax = allValues.length > 0 ? Math.max(...allValues) : 1
  const range = globalMax - globalMin || 1
  // Both strips run from the best value to the worst one. A high MGas/s
  // is a good one, so its axis descends; a long payload time is a bad
  // one, so its axis keeps ascending.
  const invert = metric === 'mgas'
  const dotLeft = (v: number) => {
    const fraction = (v - globalMin) / range
    return `${Math.max(2, Math.min(98, (invert ? 1 - fraction : fraction) * 100))}%`
  }
  const axisLeft = invert ? globalMax : globalMin
  const axisRight = invert ? globalMin : globalMax

  // The cards run from the fastest group to the slowest one, the same
  // direction as the strips. A group with no value goes last.
  const cardOrder = groupData.map((_, gi) => gi).sort((a, b) => {
    const va = values[a]
    const vb = values[b]
    if (va === undefined || vb === undefined) return Number(va === undefined) - Number(vb === undefined)
    return invert ? vb - va : va - vb
  })

  if (allValues.length === 0) return null

  return (
    <div className="flex flex-col gap-3">
      <h4 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">{title}</h4>
    {/* One card per group with the averaged value, tinted like its heatmap tile */}
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
      {cardOrder.map((gi, place) => {
        const group = groupData[gi]
        const value = values[gi]
        // Gold, silver and bronze for the fastest three, as in the ranking.
        const medal = groupData.length >= 2 && value !== undefined && place < MEDAL_CLASSES.length ? place : undefined
        const base = baselineGroupIdx >= 0 ? values[baselineGroupIdx] : undefined
        const color = heatmapColor(value, base, model)
        const isBaseline = mode === 'baseline' && gi === baselineGroupIdx && groupData.length >= 2
        const duration = durations[gi]
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
            className="relative isolate flex flex-col gap-1 overflow-hidden rounded-sm border-l-4 border-gray-300 bg-gray-100 px-3 py-2 dark:border-gray-600 dark:bg-gray-700/50"
            style={{
              ...(color ? { borderColor: color, backgroundColor: `${color}26` } : {}),
              // Same slow-payload outline as the heatmap tile.
              ...(slow ? { outline: `2px solid ${SLOW_COLOR}`, outlineOffset: '-2px' } : {}),
            }}
          >
            {medal !== undefined && (
              <Medal
                aria-hidden
                className={clsx('pointer-events-none absolute -right-3 -top-3 -z-10 size-16 rotate-12 opacity-20 dark:opacity-30', MEDAL_CLASSES[medal])}
              />
            )}
            <span
              className={clsx('inline-flex items-center gap-1.5 text-xs/5 font-medium', SLOT_TEXT_COLORS[gi % SLOT_TEXT_COLORS.length])}
              title={medal !== undefined ? `${medal + 1}. place` : undefined}
            >
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
          </div>
        )
      })}
    </div>

    {/* Strips: all groups on one axis, then one strip per group right
        under it, so the overlap between groups is visible at a glance */}
    <div className="flex flex-col gap-1">
      {groupData.length >= 2 && (
        <StripRow emphasis label={<span className="text-sm/6 font-semibold text-gray-900 dark:text-gray-100">All groups</span>}>
          {groupData.map((group, gi) =>
            group.metrics[metric].values.map((v, i) => (
              <span
                key={`${gi}-${i}`}
                className="absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full opacity-70"
                style={{ left: dotLeft(v), backgroundColor: DOT_COLORS[gi % DOT_COLORS.length] }}
                title={`${group.label}: ${fmt(v)}${metric === 'mgas' ? ' MGas/s' : ''}`}
              />
            )),
          )}
        </StripRow>
      )}
      {groupData.map((group, gi) => (
        <StripRow key={gi} label={<GroupLabel group={group} gi={gi} />}>
          {group.metrics[metric].values.map((v, i) => (
            <span
              key={i}
              className="absolute top-1/2 size-3 -translate-x-1/2 -translate-y-1/2 rounded-full opacity-70"
              style={{ left: dotLeft(v), backgroundColor: DOT_COLORS[gi % DOT_COLORS.length] }}
              title={`${fmt(v)}${metric === 'mgas' ? ' MGas/s' : ''}`}
            />
          ))}
        </StripRow>
      ))}
      {/* One shared axis under the strips */}
      <div className="flex text-xs text-gray-400">
        <span className="w-40 shrink-0" />
        <span className="flex flex-1 justify-between px-1">
          <span>{fmtAxis(axisLeft)} <span className="opacity-70">fastest</span></span>
          <span><span className="opacity-70">slowest</span> {fmtAxis(axisRight)}</span>
        </span>
      </div>
    </div>

    {/* One table with the stats of every group */}
    <table className="w-full text-xs">
      <thead>
        <tr className="border-b border-gray-200 text-left text-[10px] uppercase tracking-wide text-gray-400 dark:border-gray-700 dark:text-gray-500">
          <th className="py-1 pr-2 font-medium">Group</th>
          <th
            className="px-2 py-1 text-right font-medium"
            title={metric === 'mgas'
              ? 'Total gas over total time of the sampled runs — the throughput of the group as a whole, and the value the heatmap and the charts use. A slow run weighs as much as it lasted.'
              : 'Mean payload time of the sampled runs — the value the heatmap and the charts use.'}
          >
            Avg
          </th>
          <th className="px-2 py-1 text-right font-medium" title={`Middle value of ${unit} across the sampled runs. Less sensitive to outliers than the average.`}>Median</th>
          <th className="px-2 py-1 text-right font-medium" title={`Lowest ${unit} observed across the sampled runs.`}>Min</th>
          <th className="px-2 py-1 text-right font-medium" title={`Highest ${unit} observed across the sampled runs.`}>Max</th>
          <th className="px-2 py-1 text-right font-medium" title={`Max minus min — the spread of ${unit} across the sampled runs.`}>Range</th>
          <th className="px-2 py-1 text-right font-medium" title={`Sample standard deviation of the per-run ${unit} around their arithmetic mean. How much individual runs typically deviate.`}>σ</th>
          <th className="px-2 py-1 text-right font-medium" title={`Coefficient of Variation — standard deviation as a percentage of the arithmetic mean of the per-run ${unit}. Lower = more consistent across runs.`}>CV</th>
          <th className="px-2 py-1 text-right font-medium">Runs</th>
        </tr>
      </thead>
      <tbody className="font-mono tabular-nums text-gray-700 dark:text-gray-200">
        {groupData.map((group, gi) => {
          const m = group.metrics[metric]
          const cell = (v: number | undefined) => (v === undefined ? '—' : fmt(v))
          return (
            <tr key={gi} className="border-b border-gray-100 last:border-0 dark:border-gray-700/50">
              <td className="py-1 pr-2 font-sans"><GroupLabel group={group} gi={gi} /></td>
              <td className="px-2 py-1 text-right">{cell(m.average)}</td>
              <td className="px-2 py-1 text-right">{cell(m.median)}</td>
              <td className="px-2 py-1 text-right">{cell(m.min)}</td>
              <td className="px-2 py-1 text-right">{cell(m.max)}</td>
              <td className="px-2 py-1 text-right">{m.min !== undefined && m.max !== undefined ? fmt(m.max - m.min) : '—'}</td>
              <td className="px-2 py-1 text-right">{cell(m.stddev)}</td>
              <td className="px-2 py-1 text-right">
                {m.stddev !== undefined && m.mean !== undefined && m.mean > 0 ? `${((m.stddev / m.mean) * 100).toFixed(1)}%` : '—'}
              </td>
              <td className="px-2 py-1 text-right">{m.values.length}</td>
            </tr>
          )
        })}
      </tbody>
    </table>

    </div>
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
function StripRow({ label, children, emphasis }: { label: React.ReactNode; children: React.ReactNode; emphasis?: boolean }) {
  return (
    <div className={clsx('flex items-center gap-2', emphasis && 'mb-1')}>
      <div className="w-40 shrink-0 truncate">{label}</div>
      <div
        className={clsx(
          'relative flex-1 rounded-xs',
          // The combined strip is taller and darker, with a ring, so it
          // reads as the summary and the group strips as its parts.
          emphasis
            ? 'h-7 bg-gray-200 ring-1 ring-gray-300 dark:bg-gray-600 dark:ring-gray-500'
            : 'h-5 bg-gray-100 dark:bg-gray-700',
        )}
      >
        {children}
      </div>
    </div>
  )
}
