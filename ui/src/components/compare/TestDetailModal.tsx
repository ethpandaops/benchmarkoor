import { Fragment, useEffect, useMemo, useRef, useState } from 'react'
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
import { type HeatmapColorModel, type HeatmapMetric, heatmapColor } from './heatmapColor'
import { formatBytes, formatDuration } from '@/utils/format'
import { SLOW_COLOR, THRESHOLD_COLORS, isSlowPayload } from '@/utils/perfThreshold'

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
  /**
   * Open sections of the modal, held by the page so a shared link keeps
   * them open. `mgas` and `duration` open the group details of the
   * matching metric section, `runs` opens the table of every run.
   */
  expanded: Set<string>
  onExpandedChange: (next: Set<string>) => void
  onClose: () => void
}

// Text colour per slot, matching the group card colours of the builder,
// and the dot fill in the 500 shade of the same hues.
const SLOT_TEXT_COLORS = ['text-blue-700 dark:text-blue-300', 'text-orange-700 dark:text-orange-300', 'text-purple-700 dark:text-purple-300', 'text-green-700 dark:text-green-300', 'text-red-700 dark:text-red-300']
const DOT_COLORS = ['#3b82f6', '#f97316', '#a855f7', '#22c55e', '#ef4444']

// Dot strips. Dots that would cover each other move into lanes above and
// below the middle of the strip, so every run stays visible.
const DOT_PX = 12
const LANE_PX = 9
const MAX_LANES = 5
/** Label column of a strip row (w-40) plus the gap (gap-2). */
const STRIP_LABEL_PX = 168

type SortKey = 'group' | 'run' | 'mgas' | 'gasUsed' | 'payload' | 'duration'
type RunsGroupBy = 'group' | 'none'

/** One sampled run of a group, behind a dot of a strip. */
interface RunPoint {
  runId?: string
  timestamp?: number
  mgas?: number
  gasUsed: number
  gasUsedTime: number
  duration: number
}

interface MetricSummary {
  /** Per-run values, in run order. */
  values: number[]
  /** The run behind each value, in the same order. */
  points: RunPoint[]
  /** The group value: the same number the averaged group result and the heatmap use. */
  average?: number
  /** Arithmetic mean of the per-run values; σ and CV are defined around it. */
  mean?: number
  median?: number
  min?: number
  max?: number
  stddev?: number
}

function summarize(values: number[], average: number | undefined, points: RunPoint[]): MetricSummary {
  if (values.length === 0) return { values, points, average }
  const mean = values.reduce((a, b) => a + b, 0) / values.length
  const sorted = [...values].sort((a, b) => a - b)
  const mid = Math.floor(sorted.length / 2)
  const median = sorted.length % 2 === 0 ? (sorted[mid - 1] + sorted[mid]) / 2 : sorted[mid]
  const stddev = values.length >= 2
    ? Math.sqrt(values.reduce((sum, v) => sum + (v - mean) ** 2, 0) / (values.length - 1))
    : undefined
  return { values, points, average, mean, median, min: sorted[0], max: sorted[sorted.length - 1], stddev }
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
  expanded,
  onExpandedChange,
  onClose,
}: TestDetailModalProps) {

  const navigate = useNavigate()
  const [opcodeSort, setOpcodeSort] = useState<OpcodeSortMode>('name')

  const toggleExpanded = (token: string) => {
    const next = new Set(expanded)
    if (next.has(token)) next.delete(token); else next.add(token)
    onExpandedChange(next)
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

  // The hovered run is shared, so a dot lights up in both metric
  // sections at once. The popover stays with the section under the mouse.
  const [hoveredRunId, setHoveredRunId] = useState<string | null>(null)
  const [runsGroupBy, setRunsGroupBy] = useState<RunsGroupBy>('none')
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
      const mgas = summarize(measured.map((r) => r.mgas as number), mgasAverage, measured)
      const payloadTimes = measured.map((r) => r.gasUsedTime)
      const duration = summarize(payloadTimes, measured.length > 0 ? totalGasTime / measured.length : undefined, measured)

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
            <GroupCards groupData={groupData} mgasValues={groupMgas} durationValues={groupDurations} baselineGroupIdx={baselineGroupIdx} heatmapModel={heatmapModel} />
            <MetricSection metric="mgas" title="MGas/s" testName={testName} groupData={groupData} heatmapModel={heatmapModel} open={expanded.has('mgas')} onToggleOpen={() => toggleExpanded('mgas')} hoveredRunId={hoveredRunId} onHoverRunChange={setHoveredRunId} />
            <MetricSection metric="duration" title="Payload time" testName={testName} groupData={groupData} heatmapModel={heatmapModel} open={expanded.has('duration')} onToggleOpen={() => toggleExpanded('duration')} hoveredRunId={hoveredRunId} onHoverRunChange={setHoveredRunId} />

            <RunsTable
              groupData={groupData}
              testName={testName}
              open={expanded.has('runs')}
              onToggleOpen={() => toggleExpanded('runs')}
              groupBy={runsGroupBy}
              onGroupByChange={setRunsGroupBy}
              sortKey={sortKey}
              sortDir={sortDir}
              onSort={toggleSort}
              selectedRunIds={selectedRunIds}
              onToggleRunSelected={toggleRunSelected}
            />

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

/**
 * RunsTable lists every sampled run of every group in one table. The
 * columns sort, and the rows either cluster per group or run flat.
 */
function RunsTable({ groupData, testName, open, onToggleOpen, groupBy, onGroupByChange, sortKey, sortDir, onSort, selectedRunIds, onToggleRunSelected }: {
  groupData: GroupSummary[]
  testName: string
  /** Whether the table shows. The page owns the flag, so the URL keeps it. */
  open: boolean
  onToggleOpen: () => void
  groupBy: RunsGroupBy
  onGroupByChange: (mode: RunsGroupBy) => void
  sortKey: SortKey
  sortDir: 'asc' | 'desc'
  onSort: (key: SortKey) => void
  selectedRunIds: Set<string>
  onToggleRunSelected: (runId: string) => void
}) {
  const rows = groupData.flatMap((group, gi) => group.runs.map((run) => ({ group, gi, run })))
  if (rows.length === 0) return null

  const compare = (a: typeof rows[number], b: typeof rows[number]) => {
    let cmp = 0
    switch (sortKey) {
      case 'group': cmp = a.gi - b.gi; break
      case 'run': cmp = (a.run.timestamp ?? 0) - (b.run.timestamp ?? 0); break
      case 'mgas': cmp = (a.run.mgas ?? 0) - (b.run.mgas ?? 0); break
      case 'gasUsed': cmp = a.run.gasUsed - b.run.gasUsed; break
      case 'payload': cmp = a.run.gasUsedTime - b.run.gasUsedTime; break
      case 'duration': cmp = a.run.duration - b.run.duration; break
    }
    return sortDir === 'asc' ? cmp : -cmp
  }
  // Grouped rows keep their group together and sort inside it.
  const sorted = [...rows].sort((a, b) => (groupBy === 'group' ? a.gi - b.gi || compare(a, b) : compare(a, b)))
  const columns = groupBy === 'group' ? 6 : 7

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <button
          type="button"
          onClick={onToggleOpen}
          title={open ? 'Hide the runs' : 'Show every sampled run'}
          className="flex items-center gap-1.5 text-sm/6 font-medium text-gray-900 hover:text-gray-600 dark:text-gray-100 dark:hover:text-gray-300"
        >
          <span className={clsx('text-xs transition-transform', open && 'rotate-90')}>▶</span>
          Runs <span className="font-normal text-gray-500 dark:text-gray-400">({rows.length})</span>
        </button>
        {open && <div className="flex items-center gap-2">
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">Group by:</span>
          <div className="flex items-center gap-1 rounded-sm bg-gray-100 p-0.5 dark:bg-gray-700">
            {([['group', 'Group'], ['none', 'None']] as const).map(([value, label]) => (
              <button
                key={value}
                type="button"
                onClick={() => onGroupByChange(value)}
                className={clsx(
                  'rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                  groupBy === value
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                    : 'text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200',
                )}
              >
                {label}
              </button>
            ))}
          </div>
        </div>}
      </div>
      {open && <table className="w-full text-xs">
        <thead>
          <tr className="border-b border-gray-200 text-gray-500 dark:border-gray-700 dark:text-gray-400">
            <th className="w-6 px-2 py-1"></th>
            {groupBy === 'none' && (
              <SortableHeader label="Group" sortKey="group" currentKey={sortKey} currentDir={sortDir} onSort={onSort} align="left" />
            )}
            <SortableHeader label="Run" sortKey="run" currentKey={sortKey} currentDir={sortDir} onSort={onSort} align="left" />
            <SortableHeader label="MGas/s" sortKey="mgas" currentKey={sortKey} currentDir={sortDir} onSort={onSort} align="right" />
            <SortableHeader label="Gas Used" sortKey="gasUsed" currentKey={sortKey} currentDir={sortDir} onSort={onSort} align="right" />
            <SortableHeader label="Payload time" sortKey="payload" currentKey={sortKey} currentDir={sortDir} onSort={onSort} align="right" />
            <SortableHeader label="Total time" sortKey="duration" currentKey={sortKey} currentDir={sortDir} onSort={onSort} align="right" />
          </tr>
        </thead>
        <tbody className="text-gray-700 dark:text-gray-200">
          {sorted.map(({ group, gi, run }, ri) => {
            const isSelected = !!run.runId && selectedRunIds.has(run.runId)
            const atCap = selectedRunIds.size >= MAX_COMPARE_RUNS && !isSelected
            // One header row per group, above its first run.
            const header = groupBy === 'group' && (ri === 0 || sorted[ri - 1].gi !== gi)
              ? (
                <tr key={`head-${gi}`} className="border-b border-gray-200 bg-gray-50 dark:border-gray-700 dark:bg-gray-700/40">
                  <td colSpan={columns} className="px-2 py-1">
                    <span className="inline-flex items-center gap-2">
                      <GroupLabel group={group} gi={gi} />
                      <span className="text-gray-400 dark:text-gray-500">({group.runs.length} run{group.runs.length === 1 ? '' : 's'})</span>
                    </span>
                  </td>
                </tr>
              )
              : null
            return (
              <Fragment key={`${gi}-${ri}`}>
                {header}
                <tr
                  className="cursor-pointer border-b border-gray-100 last:border-0 hover:bg-gray-50 dark:border-gray-700/50 dark:hover:bg-gray-700/50"
                  onClick={() => { if (run.runId) window.open(`/runs/${run.runId}?testModal=${encodeURIComponent(testName)}`, '_blank') }}
                >
                  <td className="px-2 py-1" onClick={(e) => e.stopPropagation()}>
                    <input
                      type="checkbox"
                      checked={isSelected}
                      disabled={!run.runId || atCap}
                      onChange={() => run.runId && onToggleRunSelected(run.runId)}
                      title={atCap ? `Maximum ${MAX_COMPARE_RUNS} runs can be compared` : 'Select for comparison'}
                      className="size-3.5 cursor-pointer accent-blue-600 disabled:cursor-not-allowed disabled:opacity-40 dark:accent-blue-500"
                    />
                  </td>
                  {groupBy === 'none' && (
                    <td className="px-2 py-1"><GroupLabel group={group} gi={gi} /></td>
                  )}
                  <td className="px-2 py-1">{run.timestamp ? formatTimestamp(run.timestamp) : `Run ${ri + 1}`}</td>
                  <td className="px-2 py-1 text-right font-mono">{run.mgas !== undefined ? run.mgas.toFixed(2) : '-'}</td>
                  <td className="px-2 py-1 text-right font-mono">{run.gasUsed > 0 ? `${(run.gasUsed / 1_000_000).toFixed(1)}M` : '-'}</td>
                  <td className="px-2 py-1 text-right font-mono">{run.gasUsedTime > 0 ? formatDuration(run.gasUsedTime) : '-'}</td>
                  <td className="px-2 py-1 text-right font-mono" title="Wall time of the whole test step, payloads and other calls">
                    {run.duration > 0 ? formatDuration(run.duration) : '-'}
                  </td>
                </tr>
              </Fragment>
            )
          })}
        </tbody>
      </table>}
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
  /** Every sampled run of the group, in run order. */
  runs: RunPoint[]
  metrics: Record<HeatmapMetric, MetricSummary>
}

/**
 * GroupCards is one card per group, ranked from the fastest group to the
 * slowest one. A card carries both metrics: the one the heatmap colours
 * by leads, the other follows. The tint, the note and the slow marker
 * match the heatmap tile of the test.
 */
function GroupCards({ groupData, mgasValues, durationValues, baselineGroupIdx, heatmapModel }: {
  groupData: GroupSummary[]
  /** Averaged MGas/s per group — the value the heatmap tile shows. */
  mgasValues: (number | undefined)[]
  /** Averaged payload time per group, in nanoseconds. */
  durationValues: (number | undefined)[]
  baselineGroupIdx: number
  heatmapModel: HeatmapColorModel
}) {
  const { metric, mode, slowMs } = heatmapModel
  const values = metric === 'mgas' ? mgasValues : durationValues

  // The slowest group of the comparison. Every other card says how much
  // it beats that one by.
  const measured = values.filter((v): v is number => v !== undefined)
  const slowest = measured.length >= 2
    ? (metric === 'mgas' ? Math.min(...measured) : Math.max(...measured))
    : undefined

  // The gas of a test is the same for every group, so the MGas/s order is
  // the payload time order turned around. One ranking serves both.
  const order = groupData.map((_, gi) => gi).sort((a, b) => {
    const va = mgasValues[a]
    const vb = mgasValues[b]
    if (va === undefined || vb === undefined) return Number(va === undefined) - Number(vb === undefined)
    return vb - va
  })

  if (groupData.length === 0) return null

  return (
    <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-6">
      {order.map((gi, place) => {
        const group = groupData[gi]
        const mgas = mgasValues[gi]
        const duration = durationValues[gi]
        const value = values[gi]
        // Gold, silver and bronze for the fastest three, as in the ranking.
        const medal = groupData.length >= 2 && mgas !== undefined && place < MEDAL_CLASSES.length ? place : undefined
        const base = baselineGroupIdx >= 0 ? values[baselineGroupIdx] : undefined
        const color = heatmapColor(value, base, heatmapModel)
        const isBaseline = mode === 'baseline' && gi === baselineGroupIdx && groupData.length >= 2
        const slow = duration !== undefined && isSlowPayload(duration, slowMs)
        const gain = slowest !== undefined && value !== undefined && slowest > 0 && value > 0
          ? (metric === 'mgas' ? value / slowest : slowest / value) - 1
          : undefined
        const note = isBaseline
          ? 'baseline'
          : gain === undefined
            ? null
            : gain < 0.0005
              ? 'slowest'
              : `+${(gain * 100).toFixed(gain >= 0.1 ? 0 : 1)}% vs slowest`
        const lead = metric === 'mgas'
          ? { value: mgas === undefined ? '—' : mgas.toFixed(2), unit: 'MGas/s' }
          : { value: duration === undefined ? '—' : formatDuration(duration), unit: '' }
        const follow = metric === 'mgas'
          ? `${duration === undefined ? '—' : formatDuration(duration)} payload time`
          : `${mgas === undefined ? '—' : mgas.toFixed(2)} MGas/s`
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
              {lead.value}
              {lead.unit && <span className="ml-1 text-xs/5 font-normal text-gray-500 dark:text-gray-400">{lead.unit}</span>}
            </span>
            <span className="font-mono text-xs/5 text-gray-600 dark:text-gray-300">{follow}</span>
            {note && (
              <span
                className="text-xs/5 text-gray-500 dark:text-gray-400"
                title={note === 'baseline' ? 'The baseline of the comparison' : 'Against the slowest group of the comparison'}
              >
                {note}
              </span>
            )}
          </div>
        )
      })}
    </div>
  )
}

// MetricSection is the dot strips and the stats table of one metric. The
// strips carry the limit of the metric and every sampled run.
function MetricSection({ metric, title, testName, groupData, heatmapModel, open, onToggleOpen, hoveredRunId, onHoverRunChange }: {
  metric: HeatmapMetric
  title: string
  /** Test the modal shows, so a dot can open its run on that test. */
  testName: string
  groupData: GroupSummary[]
  heatmapModel: HeatmapColorModel
  /**
   * Whether the per-group strips and the stats table show. The combined
   * strip is the summary, they are its parts, so they stay behind a
   * caret. The page owns the flag so the URL keeps it.
   */
  open: boolean
  onToggleOpen: () => void
  /** Run under the mouse anywhere in the modal, highlighted in every strip. */
  hoveredRunId: string | null
  onHoverRunChange: (runId: string | null) => void
}) {
  const [hover, setHover] = useState<{ point: RunPoint; group: GroupSummary; gi: number; anchor: DOMRect } | null>(null)
  // The lanes need the width of a strip, so a dot knows how close its
  // neighbour really is.
  const stripsRef = useRef<HTMLDivElement>(null)
  const [stripWidth, setStripWidth] = useState(600)
  useEffect(() => {
    const el = stripsRef.current
    if (!el) return
    const measure = () => setStripWidth(Math.max(1, el.clientWidth - STRIP_LABEL_PX))
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(el)
    return () => observer.disconnect()
  }, [])
  const { threshold, slowMs } = heatmapModel
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
  const dotPercent = (v: number) => {
    const fraction = (v - globalMin) / range
    return Math.max(2, Math.min(98, (invert ? 1 - fraction : fraction) * 100))
  }
  const dotLeft = (v: number) => `${dotPercent(v)}%`

  // Lane of every dot of one strip, and the height that holds them.
  const spread = <T extends { value: number }>(items: T[]) => {
    const minGap = (DOT_PX / stripWidth) * 100
    const sorted = items.map((item) => ({ item, x: dotPercent(item.value) })).sort((a, b) => a.x - b.x)
    const lastX: number[] = []
    const placed = sorted.map(({ item, x }) => {
      let lane = lastX.findIndex((last) => x - last >= minGap)
      if (lane === -1) {
        if (lastX.length < MAX_LANES) {
          lane = lastX.length
          lastX.push(x)
        } else {
          // Every lane is busy, so reuse the one whose dot sits farthest left.
          lane = lastX.indexOf(Math.min(...lastX))
          lastX[lane] = x
        }
      } else {
        lastX[lane] = x
      }
      return { item, lane }
    })
    // Lane 0 keeps the middle, the others alternate above and below it.
    const half = Math.floor(lastX.length / 2)
    return { placed, height: half > 0 ? 2 * (half * LANE_PX + 8) : 0 }
  }
  const laneOffset = (lane: number) => (lane === 0 ? 0 : (lane % 2 === 1 ? -1 : 1) * Math.ceil(lane / 2) * LANE_PX)
  // Limit of the active metric, drawn on the strips: the MGas/s
  // threshold, or the slow-payload limit. It only fits when it falls
  // inside the measured range.
  const limit = metric === 'mgas' ? threshold : slowMs * 1_000_000
  const limitColor = metric === 'mgas' ? THRESHOLD_COLORS[4] : SLOW_COLOR
  const limitTitle = metric === 'mgas'
    ? `${threshold} MGas/s threshold`
    : `${formatDuration(slowMs * 1_000_000)} slow-payload limit`
  // Hovered dot: the popover anchors to it, and every dot of the same
  // run lights up, in the combined strip and in the group strip alike.
  const isActive = (point: RunPoint) =>
    point.runId !== undefined ? hoveredRunId === point.runId : hover?.point === point
  const openRun = (point: RunPoint) => {
    if (point.runId) window.open(`/runs/${point.runId}?testModal=${encodeURIComponent(testName)}`, '_blank')
  }
  const dot = (point: RunPoint, value: number, group: GroupSummary, gi: number, key: string, offset = 0) => (
    <span
      key={key}
      className={clsx(
        'absolute -translate-x-1/2 -translate-y-1/2 rounded-full transition-[width,height,opacity]',
        point.runId && 'cursor-pointer',
        isActive(point) ? 'z-10 size-4 opacity-100 ring-2 ring-gray-900/60 dark:ring-white/70' : 'size-3 opacity-70',
      )}
      style={{ top: `calc(50% + ${offset}px)`, left: dotLeft(value), backgroundColor: DOT_COLORS[gi % DOT_COLORS.length] }}
      onMouseEnter={(e) => {
        setHover({ point, group, gi, anchor: e.currentTarget.getBoundingClientRect() })
        onHoverRunChange(point.runId ?? null)
      }}
      onMouseLeave={() => {
        setHover(null)
        onHoverRunChange(null)
      }}
      onClick={() => openRun(point)}
    />
  )

  const limitInRange = limit >= globalMin && limit <= globalMax
  // Says where the limit sits when the strips cannot show the line.
  const limitNote = limitInRange
    ? null
    : metric === 'mgas'
      ? `every run is ${globalMin > limit ? 'above' : 'below'} it`
      : `every run is ${globalMax < limit ? 'under' : 'over'} it`
  const missLabel = metric === 'mgas' ? 'under the threshold' : 'over the limit'
  const limitMarker = limitInRange ? (
    <span
      className="absolute inset-y-0 w-0.5 -translate-x-1/2 rounded-full opacity-80"
      style={{ left: dotLeft(limit), backgroundColor: limitColor }}
      title={limitTitle}
    />
  ) : null
  // Both axes run from the best value to the worst one, so everything
  // right of the limit line misses it. That side carries the colour of
  // the limit. A strip where every run misses it is washed end to end.
  const everyRunMisses = metric === 'mgas' ? globalMax < limit : globalMin > limit
  const missZoneFrom = limitInRange ? dotLeft(limit) : everyRunMisses ? '0%' : null
  const missZone = missZoneFrom === null ? null : (
    <span
      className="pointer-events-none absolute inset-y-0 right-0"
      style={{ left: missZoneFrom, backgroundColor: `${limitColor}1f` }}
      title={metric === 'mgas'
        ? `Under the ${threshold} MGas/s threshold`
        : `Over the ${formatDuration(slowMs * 1_000_000)} slow-payload limit`}
    />
  )
  const stripMarkers = (
    <>
      {missZone}
      {limitMarker}
    </>
  )

  // Every run of every group on the combined strip, laid out together.
  const combined = spread(groupData.flatMap((group, gi) =>
    group.metrics[metric].values.map((value, i) => ({
      value,
      point: group.metrics[metric].points[i],
      group,
      gi,
      key: `${gi}-${i}`,
    })),
  ))

  const showDetails = open || groupData.length < 2
  const axisLeft = invert ? globalMax : globalMin
  const axisRight = invert ? globalMin : globalMax

  if (allValues.length === 0) return null

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <h4 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">{title}</h4>
        {/* Legend of the limit line and of the side that misses it */}
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs/5 text-gray-500 dark:text-gray-400">
          <span className="flex items-center gap-1.5">
            <span className="inline-block h-3 w-0.5 rounded-full" style={{ backgroundColor: limitColor }} />
            {limitTitle}
            {limitNote && <span className="opacity-70">· {limitNote}</span>}
          </span>
          <span className="flex items-center gap-1.5">
            <span className="inline-block size-3 rounded-xs" style={{ backgroundColor: `${limitColor}1f` }} />
            {missLabel}
          </span>
        </div>
      </div>
    {/* Strips: all groups on one axis, and, on request, one strip per
        group right under it, so the overlap between groups is visible */}
    <div ref={stripsRef} className="flex flex-col gap-1">
      {groupData.length >= 2 && (
        <StripRow
          emphasis
          height={combined.height}
          marker={stripMarkers}
          label={
            <button
              type="button"
              onClick={onToggleOpen}
              title={open ? 'Hide the strip and the stats of every group' : 'Show the strip and the stats of every group'}
              className="flex items-center gap-1.5 text-sm/6 font-semibold text-gray-900 hover:text-gray-600 dark:text-gray-100 dark:hover:text-gray-300"
            >
              <span className={clsx('text-xs transition-transform', open && 'rotate-90')}>▶</span>
              All groups
              <span className="text-xs/5 font-normal text-gray-500 dark:text-gray-400">({groupData.length})</span>
            </button>
          }
        >
          {combined.placed.map(({ item, lane }) =>
            dot(item.point, item.value, item.group, item.gi, item.key, laneOffset(lane)),
          )}
        </StripRow>
      )}
      {showDetails && groupData.map((group, gi) => {
        const own = spread(group.metrics[metric].values.map((value, i) => ({
          value,
          point: group.metrics[metric].points[i],
          key: String(i),
        })))
        return (
          <StripRow key={gi} height={own.height} marker={stripMarkers} label={<GroupLabel group={group} gi={gi} />}>
            {own.placed.map(({ item, lane }) => dot(item.point, item.value, group, gi, item.key, laneOffset(lane)))}
          </StripRow>
        )
      })}
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
    {showDetails && <table className="w-full text-xs">
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
    </table>}

    {hover && (
      <div
        className="pointer-events-none fixed z-[60] w-56 -translate-x-1/2 -translate-y-full rounded-sm border border-gray-200 bg-white p-2 text-xs shadow-lg dark:border-gray-600 dark:bg-gray-800"
        style={{
          left: Math.max(120, Math.min(window.innerWidth - 120, hover.anchor.left + hover.anchor.width / 2)),
          top: hover.anchor.top - 8,
        }}
      >
        <div className="truncate"><GroupLabel group={hover.group} gi={hover.gi} /></div>
        <div className="mt-1 grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 font-mono text-gray-700 dark:text-gray-200">
          <span className="font-sans text-gray-500 dark:text-gray-400">MGas/s</span>
          <span className="text-right">{hover.point.mgas !== undefined ? hover.point.mgas.toFixed(2) : '—'}</span>
          <span className="font-sans text-gray-500 dark:text-gray-400">Payload time</span>
          <span className="text-right">{formatDuration(hover.point.gasUsedTime)}</span>
          <span className="font-sans text-gray-500 dark:text-gray-400">Total time</span>
          <span className="text-right">{formatDuration(hover.point.duration)}</span>
          <span className="font-sans text-gray-500 dark:text-gray-400">Gas used</span>
          <span className="text-right">{(hover.point.gasUsed / 1_000_000).toFixed(1)}M</span>
        </div>
        {hover.point.timestamp !== undefined && (
          <div className="mt-1 text-gray-500 dark:text-gray-400">{formatTimestamp(hover.point.timestamp)}</div>
        )}
        {hover.point.runId && (
          <div className="text-gray-400 dark:text-gray-500">Click to open the run in a new tab</div>
        )}
      </div>
    )}

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
function StripRow({ label, children, emphasis, marker, height }: {
  label: React.ReactNode
  children: React.ReactNode
  emphasis?: boolean
  /** Limit line of the metric, drawn under the dots. */
  marker?: React.ReactNode
  /** Taller row in pixels, when the dots need lanes. Zero keeps the default. */
  height?: number
}) {
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
        style={height ? { height } : undefined}
      >
        {marker}
        {children}
      </div>
    </div>
  )
}
