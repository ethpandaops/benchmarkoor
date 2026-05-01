import { useEffect, useMemo, useRef, useState } from 'react'
import { useQueries } from '@tanstack/react-query'
import clsx from 'clsx'
import { Check, Copy, Download } from 'lucide-react'
import type { TestEntry, SuiteTest, AggregatedStats, MethodsAggregated, StepResult, PostTestRPCCallConfig } from '@/api/types'
import { fetchHead } from '@/api/client'
import { Modal } from '@/components/shared/Modal'
import { TimeBreakdown } from './TimeBreakdown'
import { MGasBreakdown } from './MGasBreakdown'
import { ExecutionsList } from './ExecutionsList'
import { BlockLogDetails } from './BlockLogDetails'
import type { TestStatusFilter } from './TestsTable'
import { type StepTypeOption, ALL_STEP_TYPES } from '@/pages/RunDetailPage'
import { formatDuration, formatBytes } from '@/utils/format'
import { EESTInfoContent, type OpcodeSortMode } from '@/components/suite-detail/TestFilesList'
import { OpcodeDiffPanel, type OpcodeDiffRow } from './OpcodeDiffPanel'
import { useBlockLogs } from '@/api/hooks/useBlockLogs'

// Aggregate stats from selected steps of a test entry
function getAggregatedStats(entry: TestEntry, stepFilter: StepTypeOption[] = ALL_STEP_TYPES): AggregatedStats | undefined {
  if (!entry.steps) return undefined

  // Build array of steps based on filter
  const stepMap: Record<StepTypeOption, StepResult | undefined> = {
    setup: entry.steps.setup,
    test: entry.steps.test,
    cleanup: entry.steps.cleanup,
  }

  const steps = stepFilter
    .map((type) => stepMap[type])
    .filter((s): s is StepResult => s?.aggregated !== undefined)

  if (steps.length === 0) return undefined

  // Sum up stats from all steps
  let timeTotal = 0
  let gasUsedTotal = 0
  let gasUsedTimeTotal = 0
  let success = 0
  let fail = 0

  // Merge method stats from all steps
  const mergedTimes: Record<string, { count: number; last: number; min?: number; max?: number; mean?: number; p50?: number; p95?: number; p99?: number }> = {}
  const mergedMgasS: Record<string, { count: number; last: number; min?: number; max?: number; mean?: number; p50?: number; p95?: number; p99?: number }> = {}

  for (const step of steps) {
    if (step?.aggregated) {
      timeTotal += step.aggregated.time_total
      gasUsedTotal += step.aggregated.gas_used_total
      gasUsedTimeTotal += step.aggregated.gas_used_time_total
      success += step.aggregated.success
      fail += step.aggregated.fail

      // Merge times
      for (const [method, stats] of Object.entries(step.aggregated.method_stats.times)) {
        if (!mergedTimes[method]) {
          mergedTimes[method] = { ...stats }
        } else {
          mergedTimes[method].count += stats.count
          mergedTimes[method].last = stats.last
        }
      }

      // Merge mgas_s
      for (const [method, stats] of Object.entries(step.aggregated.method_stats.mgas_s)) {
        if (!mergedMgasS[method]) {
          mergedMgasS[method] = { ...stats }
        } else {
          mergedMgasS[method].count += stats.count
          mergedMgasS[method].last = stats.last
        }
      }
    }
  }

  return {
    time_total: timeTotal,
    gas_used_total: gasUsedTotal,
    gas_used_time_total: gasUsedTimeTotal,
    success,
    fail,
    msg_count: 0,
    method_stats: { times: mergedTimes, mgas_s: mergedMgasS } as MethodsAggregated,
  }
}

export type SortMode = 'order' | 'mgas' | 'gas'
export type GroupMode = 'none' | 'gas'

const GAS_GROUP_STEP = 30_000_000 // 30M gas per group

function getGasGroup(gasUsed: number): number {
  return Math.round(gasUsed / GAS_GROUP_STEP) * GAS_GROUP_STEP
}

function formatGasGroup(gasGroup: number): string {
  return `${Math.round(gasGroup / 1_000_000)}M`
}

interface TestHeatmapProps {
  tests: Record<string, TestEntry>
  suiteTests?: SuiteTest[]
  /**
   * Per-test opcode diffs between this run's extracted counts and the
   * suite-defined counts. When the selected test has an entry, a
   * compact diff table is rendered inside its modal. Caller (the run
   * detail page) computes this from useRunOpcodes + suite.tests.
   */
  opcodeDiffByTest?: Record<string, OpcodeDiffRow[]>
  runId: string
  suiteHash?: string
  selectedTest?: string
  statusFilter?: TestStatusFilter
  searchQuery?: string
  sortMode?: SortMode
  groupMode?: GroupMode
  threshold?: number
  stepFilter?: StepTypeOption[]
  postTestRPCCalls?: PostTestRPCCallConfig[]
  // inProgressTestKey, when set, marks one tile to pulse blue
  // continuously — used by the live view to show "the runner is
  // working on this test right now". Caller (LiveRunDetailView) picks
  // the lowest-order un-processed tile while status === 'running'.
  inProgressTestKey?: string
  onSelectedTestChange?: (testName: string | undefined) => void
  onSortModeChange?: (mode: SortMode) => void
  onGroupModeChange?: (mode: GroupMode) => void
  onThresholdChange?: (threshold: number) => void
  onSearchChange?: (query: string) => void
  activeStepTab?: 'test' | 'setup' | 'cleanup'
  onActiveStepTabChange?: (tab: 'test' | 'setup' | 'cleanup') => void
  expandedExecRows?: Set<number>
  onExpandedExecRowsChange?: (rows: Set<number>) => void
}

const COLORS = [
  '#22c55e', // green - best (highest MGas/s)
  '#84cc16', // lime
  '#eab308', // yellow
  '#f97316', // orange
  '#ef4444', // red - worst (lowest MGas/s)
]

const MIN_THRESHOLD = 10
const MAX_THRESHOLD = 1000
const DEFAULT_THRESHOLD = 60

function calculateMGasPerSec(gasUsedTotal: number, gasUsedTimeTotal: number): number | undefined {
  if (gasUsedTimeTotal <= 0 || gasUsedTotal <= 0) return undefined
  return (gasUsedTotal * 1000) / gasUsedTimeTotal
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <button
      onClick={handleCopy}
      className="shrink-0 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
      title="Copy to clipboard"
    >
      {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
    </button>
  )
}

function getColorByThreshold(value: number, threshold: number): string {
  // Scale: 0 = threshold (yellow), >threshold = green, <threshold = red
  // Range: 0 to 2*threshold maps to full color scale
  const ratio = value / threshold
  if (ratio >= 2) return COLORS[0] // Very fast - green
  if (ratio >= 1.5) return COLORS[1] // Fast - lime
  if (ratio >= 1) return COLORS[2] // At threshold - yellow
  if (ratio >= 0.5) return COLORS[3] // Slow - orange
  return COLORS[4] // Very slow - red
}

interface TestData {
  testKey: string
  filename: string
  order: number
  mgasPerSec: number
  gasUsedTotal: number
  gasUsedTimeTotal: number
  hasFail: boolean
  noData: boolean
  // notProcessed is true when the test is part of the suite but no
  // result entry exists for it yet (live run still in progress, run
  // canceled, etc). Distinct from noData (test ran but produced no gas
  // data) so the cell can show a more faded "didn't run" style.
  notProcessed: boolean
}

// Diagonal stripe pattern for tests that ran but didn't produce gas data
// (e.g. failed at setup before the test step).
const NO_DATA_STYLE = {
  backgroundColor: '#374151',
  backgroundImage: 'repeating-linear-gradient(45deg, transparent, transparent 2px, #1f2937 2px, #1f2937 4px)',
}

// Faded style for tests that exist in the suite but haven't been
// processed yet (in-progress live runs, canceled runs, etc).
const NOT_PROCESSED_STYLE = {
  backgroundColor: 'rgba(156, 163, 175, 0.15)',
}

function PostTestDumps({ runId, testName, calls }: { runId: string; testName: string; calls: PostTestRPCCallConfig[] }) {
  const dumpCalls = calls.filter((c) => c.dump?.enabled && c.dump.filename)

  const fileQueries = useQueries({
    queries: dumpCalls.map((call) => ({
      queryKey: ['post-test-dump', runId, testName, call.dump!.filename],
      queryFn: () => fetchHead(`runs/${runId}/${testName}/post_test_rpc_calls/${call.dump!.filename}.json`),
      staleTime: Infinity,
    })),
  })

  if (dumpCalls.length === 0) return null

  return (
    <div className="flex flex-col gap-2">
      <div className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Post-Test RPC Dumps</div>
      <div className="overflow-x-auto rounded-xs bg-gray-100 dark:bg-gray-900">
        <table className="w-full text-left text-xs/5">
          <thead>
            <tr className="border-b border-gray-200 text-gray-500 dark:border-gray-700 dark:text-gray-400">
              <th className="px-3 py-2 font-medium">Method</th>
              <th className="px-3 py-2 font-medium">Params</th>
              <th className="px-3 py-2 font-medium">File</th>
              <th className="px-3 py-2 text-right font-medium">Size</th>
              <th className="px-3 py-2" />
            </tr>
          </thead>
          <tbody className="font-mono text-gray-900 dark:text-gray-100">
            {dumpCalls.map((call, i) => {
              const query = fileQueries[i]
              const fileInfo = query?.data
              return (
                <tr key={i} className="border-b border-gray-200 last:border-0 dark:border-gray-700">
                  <td className="px-3 py-2">{call.method}</td>
                  <td className="max-w-48 truncate px-3 py-2 text-gray-500 dark:text-gray-400">
                    {call.params && call.params.length > 0 ? JSON.stringify(call.params) : '-'}
                  </td>
                  <td className="px-3 py-2">{call.dump!.filename}.json</td>
                  <td className="px-3 py-2 text-right text-gray-500 dark:text-gray-400">
                    {query?.isLoading ? '...' : fileInfo?.exists && fileInfo.size != null ? formatBytes(fileInfo.size) : '-'}
                  </td>
                  <td className="px-3 py-2">
                    {fileInfo?.exists ? (
                      <a
                        href={fileInfo.url}
                        download={`${call.dump!.filename}.json`}
                        className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                        title="Download"
                      >
                        <Download className="size-4" />
                      </a>
                    ) : null}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function HeatmapCell({
  test,
  threshold,
  statusFilter,
  searchQuery,
  popDelayMs,
  isInProgress,
  onSelect,
  onMouseEnter,
  onMouseLeave,
}: {
  test: TestData
  threshold: number
  statusFilter: TestStatusFilter
  searchQuery: string
  // popDelayMs is set when this tile just transitioned from
  // not-processed to having a result, driving a brief scale + brightness
  // flash. Undefined means no animation (steady state). The delay is
  // staggered across newly-completed tiles so they pop one after another.
  popDelayMs?: number
  // isInProgress turns on an infinite blue pulse for the tile that
  // represents the test the runner is most likely currently executing.
  // Pop animation takes precedence when both are set (a tile transitions
  // from "in progress" to "completed" on the next snapshot).
  isInProgress?: boolean
  onSelect: (testKey: string) => void
  onMouseEnter: (test: TestData, event: React.MouseEvent) => void
  onMouseLeave: () => void
}) {
  const matchesStatusFilter =
    statusFilter === 'all' ||
    // Not-processed tiles are neither "passed" nor "failed" — exclude
    // them from those filters so the user can isolate ran-and-passed /
    // ran-and-failed tests cleanly.
    (!test.notProcessed && statusFilter === 'passed' && !test.hasFail) ||
    (!test.notProcessed && statusFilter === 'failed' && test.hasFail)
  const matchesSearchQuery = !searchQuery || test.testKey.toLowerCase().includes(searchQuery.toLowerCase())
  const matchesFilter = matchesStatusFilter && matchesSearchQuery
  let baseStyle: React.CSSProperties
  if (test.notProcessed) {
    baseStyle = NOT_PROCESSED_STYLE
  } else if (test.noData) {
    baseStyle = NO_DATA_STYLE
  } else {
    baseStyle = { backgroundColor: getColorByThreshold(test.mgasPerSec, threshold) }
  }
  const style: React.CSSProperties = {
    ...baseStyle,
    transition: 'background-color 0.4s ease-out',
    ...(matchesFilter ? {} : { opacity: 0.2 }),
  }

  // Animation precedence: a freshly-popping tile takes priority over
  // the "in progress" pulse, since it represents the in-progress tile
  // transitioning into a completed one on the latest snapshot.
  if (popDelayMs !== undefined) {
    style.animation = 'heatmapPop 0.6s ease-out both'
    style.animationDelay = `${popDelayMs}ms`
  } else if (isInProgress) {
    style.animation = 'liveTilePulse 1.2s ease-in-out infinite'
  }

  return (
    <button
      key={test.testKey}
      onClick={() => onSelect(test.testKey)}
      onMouseEnter={(e) => onMouseEnter(test, e)}
      onMouseLeave={onMouseLeave}
      className={clsx(
        'size-3 cursor-pointer rounded-xs transition-all hover:scale-150 hover:ring-2 hover:ring-gray-400 dark:hover:ring-gray-500',
        test.hasFail && 'ring-1 ring-red-500',
      )}
      style={style}
    />
  )
}

export function TestHeatmap({
  tests,
  suiteTests,
  opcodeDiffByTest,
  runId,
  suiteHash,
  selectedTest,
  statusFilter = 'all',
  searchQuery = '',
  sortMode: sortModeProp,
  groupMode: groupModeProp,
  threshold: thresholdProp,
  stepFilter = ALL_STEP_TYPES,
  postTestRPCCalls,
  inProgressTestKey,
  onSelectedTestChange,
  onSortModeChange,
  onGroupModeChange,
  onThresholdChange,
  activeStepTab: activeStepTabProp,
  onActiveStepTabChange,
  expandedExecRows,
  onExpandedExecRowsChange,
}: TestHeatmapProps) {
  const sortMode = sortModeProp ?? 'order'
  const groupMode = groupModeProp ?? 'none'
  const threshold = thresholdProp ?? DEFAULT_THRESHOLD
  const [tooltip, setTooltip] = useState<{ test: TestData; x: number; y: number } | null>(null)
  const [opcodeSort, setOpcodeSort] = useState<OpcodeSortMode>('name')
  const [activeStepTabLocal, setActiveStepTabLocal] = useState<'test' | 'setup' | 'cleanup'>('test')
  const activeStepTab = activeStepTabProp ?? activeStepTabLocal
  const setActiveStepTab = (tab: 'test' | 'setup' | 'cleanup') => {
    setActiveStepTabLocal(tab)
    onActiveStepTabChange?.(tab)
  }
  const { data: blockLogs } = useBlockLogs(runId)

  // Pop-in stagger state for newly-completed tiles. Populated below by
  // an effect that diffs the latest testData against the previous
  // completed set; declared up here so it's in scope when we render the
  // cell list further down.
  const prevCompletedRef = useRef<Set<string> | null>(null)
  const [popDelays, setPopDelays] = useState<Map<string, number>>(new Map())

  const handleSortModeChange = (mode: SortMode) => {
    onSortModeChange?.(mode)
  }

  const handleGroupModeChange = (mode: GroupMode) => {
    onGroupModeChange?.(mode)
  }

  const handleThresholdChange = (value: number) => {
    if (value >= MIN_THRESHOLD && value <= MAX_THRESHOLD) {
      onThresholdChange?.(value)
    }
  }

  const executionOrder = useMemo(() => {
    if (!suiteTests) return new Map<string, number>()
    return new Map(suiteTests.map((test, index) => [test.name, index + 1]))
  }, [suiteTests])

  const genesisMap = useMemo(() => {
    if (!suiteTests) return new Map<string, string>()
    const m = new Map<string, string>()
    for (const test of suiteTests) {
      if (test.genesis) m.set(test.name, test.genesis)
    }
    return m
  }, [suiteTests])

  const { testData, minMgas, maxMgas } = useMemo(() => {
    const data: TestData[] = []
    let minMgas = Infinity
    let maxMgas = -Infinity

    // Union of suite tests and tests with results, so the heatmap always
    // shows a tile for every planned test in the suite — even if the run
    // hasn't reached it yet (live), got canceled before it ran, or only
    // a subset ran. Tests in `tests` but missing from `suiteTests`
    // (shouldn't normally happen) are still included so we don't lose data.
    const allTestNames = new Set<string>(Object.keys(tests))
    if (suiteTests) {
      for (const t of suiteTests) allTestNames.add(t.name)
    }

    for (const testName of allTestNames) {
      const entry = tests[testName]
      const order = executionOrder.get(testName) ?? Infinity

      if (!entry) {
        // Suite test with no result → tile for visual completeness only.
        data.push({
          testKey: testName,
          filename: testName,
          order,
          mgasPerSec: 0,
          gasUsedTotal: 0,
          gasUsedTimeTotal: 0,
          hasFail: false,
          noData: true,
          notProcessed: true,
        })

        continue
      }

      // Use stepFilter for MGas/s calculation
      const statsFiltered = getAggregatedStats(entry, stepFilter)
      // Use all steps for hasFail indicator
      const statsAll = getAggregatedStats(entry, ALL_STEP_TYPES)
      const mgasPerSec = statsFiltered ? calculateMGasPerSec(statsFiltered.gas_used_total, statsFiltered.gas_used_time_total) : undefined
      const noData = mgasPerSec === undefined

      if (!noData) {
        minMgas = Math.min(minMgas, mgasPerSec)
        maxMgas = Math.max(maxMgas, mgasPerSec)
      }

      data.push({
        testKey: testName,
        filename: testName,
        order,
        mgasPerSec: mgasPerSec ?? 0,
        gasUsedTotal: statsFiltered?.gas_used_total ?? 0,
        gasUsedTimeTotal: statsFiltered?.gas_used_time_total ?? 0,
        hasFail: statsAll ? statsAll.fail > 0 : false,
        noData,
        notProcessed: false,
      })
    }

    if (minMgas === Infinity) minMgas = 0
    if (maxMgas === -Infinity) maxMgas = 0

    return { testData: data, minMgas, maxMgas }
  }, [tests, suiteTests, executionOrder, stepFilter])

  // Animate tiles that just transitioned from "not processed" to having
  // a result by giving each one a staggered pop-in. We diff against the
  // set of completed keys from the previous render — kept in a ref —
  // and assign each newly-completed tile a delay so they cascade in
  // execution order. The first render seeds the ref without animating
  // anything (avoids flashing every existing tile on initial paint).
  useEffect(() => {
    const completed = new Set<string>()
    const ordered: { key: string; order: number }[] = []
    for (const t of testData) {
      if (!t.notProcessed) {
        completed.add(t.testKey)
        ordered.push({ key: t.testKey, order: t.order })
      }
    }

    if (prevCompletedRef.current === null) {
      prevCompletedRef.current = completed

      return
    }

    const newly = ordered
      .filter(({ key }) => !prevCompletedRef.current!.has(key))
      .sort((a, b) => a.order - b.order)
    prevCompletedRef.current = completed

    if (newly.length === 0) return

    // Stagger 60ms per tile, capped so a huge burst still finishes
    // within ~2s. Tiles past the cap all start at the cap delay.
    const stepMs = 60
    const maxStaggerMs = 1800
    const map = new Map<string, number>()
    for (let i = 0; i < newly.length; i++) {
      map.set(newly[i].key, Math.min(i * stepMs, maxStaggerMs))
    }

    // Intentional cascading render: this is a one-shot animation
    // signal derived from a diff against the previous render's state,
    // not steady-state derived data. Each testData change emits exactly
    // two follow-up renders (set delays, then clear them after the
    // pop), bounded and small.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setPopDelays(map)

    const longest = Math.min((newly.length - 1) * stepMs, maxStaggerMs) + 600 + 100
    const t = setTimeout(() => setPopDelays(new Map()), longest)

    return () => clearTimeout(t)
  }, [testData])

  const sortedData = useMemo(() => {
    const sorted = [...testData]
    if (sortMode === 'order') {
      sorted.sort((a, b) => a.order - b.order)
    } else if (sortMode === 'gas') {
      sorted.sort((a, b) => b.gasUsedTotal - a.gasUsedTotal) // most gas first
    } else {
      sorted.sort((a, b) => a.mgasPerSec - b.mgasPerSec) // slowest first
    }
    return sorted
  }, [testData, sortMode])

  const groupedData = useMemo(() => {
    if (groupMode === 'none') return null

    const groups = new Map<number, TestData[]>()
    for (const test of sortedData) {
      const group = getGasGroup(test.gasUsedTotal)
      const existing = groups.get(group)
      if (existing) {
        existing.push(test)
      } else {
        groups.set(group, [test])
      }
    }

    // Sort groups by gas amount descending
    return [...groups.entries()]
      .sort(([a], [b]) => b - a)
      .map(([gasGroup, items]) => ({
        label: formatGasGroup(gasGroup),
        gasGroup,
        tests: items,
      }))
  }, [sortedData, groupMode])

  const histogramData = useMemo(() => {
    const testsWithData = testData.filter((t) => !t.noData)
    if (testsWithData.length === 0) return []

    // Create bins based on threshold: 0, 0.25x, 0.5x, 0.75x, 1x, 1.25x, 1.5x, 1.75x, 2x, 2.5x, 3x+
    const binMultipliers = [0, 0.25, 0.5, 0.75, 1, 1.25, 1.5, 1.75, 2, 2.5, 3]
    const bins = Array(binMultipliers.length).fill(0)

    for (const test of testsWithData) {
      const ratio = test.mgasPerSec / threshold
      let binIndex = binMultipliers.findIndex((_, i) => {
        const next = binMultipliers[i + 1]
        return next === undefined ? true : ratio < next
      })
      if (binIndex === -1) binIndex = binMultipliers.length - 1
      bins[binIndex]++
    }

    const maxCount = Math.max(...bins)
    const logMax = Math.log10(maxCount + 1)
    return bins.map((count, i) => {
      const rangeStart = binMultipliers[i] * threshold
      const rangeEnd = binMultipliers[i + 1] !== undefined ? binMultipliers[i + 1] * threshold : Infinity
      const midpoint = binMultipliers[i + 1] !== undefined
        ? (binMultipliers[i] + binMultipliers[i + 1]) / 2 * threshold
        : binMultipliers[i] * 1.25 * threshold
      return {
        count,
        height: maxCount > 0 ? (Math.log10(count + 1) / logMax) * 100 : 0,
        rangeStart,
        rangeEnd,
        label: binMultipliers[i + 1] !== undefined
          ? `${rangeStart.toFixed(0)}-${rangeEnd.toFixed(0)}`
          : `${rangeStart.toFixed(0)}+`,
        color: getColorByThreshold(midpoint, threshold),
      }
    })
  }, [testData, threshold])

  const handleMouseEnter = (test: TestData, event: React.MouseEvent) => {
    const rect = event.currentTarget.getBoundingClientRect()
    setTooltip({
      test,
      x: rect.left + rect.width / 2,
      y: rect.top,
    })
  }

  const handleMouseLeave = () => {
    setTooltip(null)
  }

  const handleSelect = (testKey: string) => {
    onSelectedTestChange?.(testKey)
  }

  if (testData.length === 0) {
    return (
      <div className="flex h-32 items-center justify-center text-sm/6 text-gray-500 dark:text-gray-400">
        No MGas/s data available
      </div>
    )
  }

  return (
    <div className="relative flex flex-col gap-4">
      {/* Controls */}
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-4">
          <div className="flex items-center gap-2">
            <span className="text-xs/5 text-gray-500 dark:text-gray-400">Sort by:</span>
            <div className="flex items-center gap-1 rounded-sm bg-gray-100 p-0.5 dark:bg-gray-700">
              <button
                onClick={() => handleSortModeChange('order')}
                className={clsx(
                  'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                  sortMode === 'order'
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                    : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                )}
              >
                Test #
              </button>
              <button
                onClick={() => handleSortModeChange('mgas')}
                className={clsx(
                  'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                  sortMode === 'mgas'
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                    : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                )}
              >
                MGas/s
              </button>
              <button
                onClick={() => handleSortModeChange('gas')}
                className={clsx(
                  'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                  sortMode === 'gas'
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                    : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                )}
              >
                Gas Used
              </button>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-xs/5 text-gray-500 dark:text-gray-400">Group by:</span>
            <div className="flex items-center gap-1 rounded-sm bg-gray-100 p-0.5 dark:bg-gray-700">
              <button
                onClick={() => handleGroupModeChange('none')}
                className={clsx(
                  'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                  groupMode === 'none'
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                    : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                )}
              >
                None
              </button>
              <button
                onClick={() => handleGroupModeChange('gas')}
                className={clsx(
                  'rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                  groupMode === 'gas'
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                    : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                )}
              >
                Gas Used
              </button>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <span className="text-xs/5 text-gray-500 dark:text-gray-400">Slow threshold:</span>
            <input
              type="range"
              min={MIN_THRESHOLD}
              max={MAX_THRESHOLD}
              value={threshold}
              onChange={(e) => handleThresholdChange(Number(e.target.value))}
              className="h-1.5 w-24 cursor-pointer appearance-none rounded-full bg-gray-200 accent-blue-500 dark:bg-gray-700"
            />
            <input
              type="number"
              min={MIN_THRESHOLD}
              max={MAX_THRESHOLD}
              value={threshold}
              onChange={(e) => handleThresholdChange(Number(e.target.value))}
              className="w-16 rounded-sm border border-gray-300 bg-white px-1.5 py-0.5 text-center text-xs/5 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
            />
            <span className="text-xs/5 text-gray-500 dark:text-gray-400">MGas/s</span>
            {threshold !== DEFAULT_THRESHOLD && (
              <button
                onClick={() => handleThresholdChange(DEFAULT_THRESHOLD)}
                className="text-xs/5 text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
              >
                Reset
              </button>
            )}
          </div>
        </div>
        <div className="text-xs/5 text-gray-500 dark:text-gray-400">
          {(() => {
            const notProcessed = testData.filter((t) => t.notProcessed).length
            if (notProcessed > 0) {
              return `${testData.length - notProcessed}/${testData.length} tests processed | ${minMgas.toFixed(1)} - ${maxMgas.toFixed(1)} MGas/s`
            }
            return `${testData.length} tests | ${minMgas.toFixed(1)} - ${maxMgas.toFixed(1)} MGas/s`
          })()}
        </div>
      </div>

      {/* Heatmap Grid */}
      <div className="flex flex-col gap-1">
        <div className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">
          Tests {sortMode === 'order' ? '(by execution order)' : sortMode === 'gas' ? '(by gas used, most first)' : '(by MGas/s, slowest first)'}
          {groupMode !== 'none' && ' — grouped by gas used (30M steps)'}
        </div>
        {groupedData ? (
          <div className="flex flex-col gap-2">
            {groupedData.map((group) => (
              <div key={group.gasGroup} className="flex flex-col gap-0.5">
                <div className="flex items-center gap-2">
                  <span className="text-xs/5 font-medium text-gray-600 dark:text-gray-300">{group.label} gas</span>
                  <span className="text-xs/5 text-gray-400 dark:text-gray-500">({group.tests.length})</span>
                </div>
                <div className="flex flex-wrap gap-0.5">
                  {group.tests.map((test) => (
                    <HeatmapCell
                      key={test.testKey}
                      test={test}
                      threshold={threshold}
                      statusFilter={statusFilter}
                      searchQuery={searchQuery}
                      popDelayMs={popDelays.get(test.testKey)}
                      isInProgress={test.testKey === inProgressTestKey}
                      onSelect={handleSelect}
                      onMouseEnter={handleMouseEnter}
                      onMouseLeave={handleMouseLeave}
                    />
                  ))}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <div className="flex flex-wrap gap-0.5">
            {sortedData.map((test) => (
              <HeatmapCell
                key={test.testKey}
                test={test}
                threshold={threshold}
                statusFilter={statusFilter}
                searchQuery={searchQuery}
                popDelayMs={popDelays.get(test.testKey)}
                isInProgress={test.testKey === inProgressTestKey}
                onSelect={handleSelect}
                onMouseEnter={handleMouseEnter}
                onMouseLeave={handleMouseLeave}
              />
            ))}
          </div>
        )}
      </div>

      {/* Histogram */}
      <div className="flex flex-col gap-1">
        <div className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Distribution (by threshold multiples)</div>
        <div className="flex items-end gap-1">
          <div className="flex h-16 w-8 shrink-0 flex-col items-center justify-end">
            <span className="text-xs/5 font-medium text-red-600 dark:text-red-400">
              {testData.filter((t) => !t.noData && t.mgasPerSec < threshold).length}
            </span>
            <span className="text-xs/5 text-gray-400 dark:text-gray-500">slow</span>
          </div>
          <div className="relative flex h-16 flex-1 items-end gap-1">
            {histogramData.map((bin, i) => (
              <div
                key={i}
                className="flex-1 rounded-t-xs transition-all hover:opacity-80"
                style={{
                  height: `${bin.height}%`,
                  backgroundColor: bin.color,
                  minHeight: bin.count > 0 ? '2px' : '0',
                }}
                title={`${bin.label} MGas/s: ${bin.count} tests`}
              />
            ))}
            {/* Threshold line - positioned at bin index 4 (1x threshold) */}
            <div
              className="absolute bottom-0 top-0 w-0.5 bg-black dark:bg-white"
              style={{ left: `${(4 / 11) * 100}%` }}
              title={`Threshold: ${threshold} MGas/s`}
            />
          </div>
          <div className="flex h-16 w-8 shrink-0 flex-col items-center justify-end">
            <span className="text-xs/5 font-medium text-green-600 dark:text-green-400">
              {testData.filter((t) => !t.noData && t.mgasPerSec >= threshold).length}
            </span>
            <span className="text-xs/5 text-gray-400 dark:text-gray-500">fast</span>
          </div>
        </div>
        <div className="flex justify-between px-9 text-xs/5 text-gray-400 dark:text-gray-500">
          <span>0</span>
          <span className="font-medium text-yellow-600 dark:text-yellow-400">{threshold} MGas/s (threshold)</span>
          <span>{threshold * 3}+</span>
        </div>
      </div>

      {/* Legend */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs/5 text-gray-500 dark:text-gray-400">
        <span className="flex items-center gap-1">
          <span>&gt;{threshold * 2}</span>
          <span className="flex gap-0.5">
            {COLORS.map((color, i) => (
              <span key={i} className="size-3 rounded-xs" style={{ backgroundColor: color }} />
            ))}
          </span>
          <span>&lt;{threshold / 2}</span>
        </span>
        <span className="text-gray-400 dark:text-gray-500">({threshold} MGas/s = yellow)</span>
        <span>
          <span className="mr-1 inline-block size-3 rounded-xs" style={NO_DATA_STYLE} />
          No data
        </span>
        <span>
          <span className="mr-1 inline-block size-3 rounded-xs ring-1 ring-gray-300 dark:ring-gray-700" style={NOT_PROCESSED_STYLE} />
          Not processed
        </span>
        <span>
          <span className="mr-1 inline-block size-3 rounded-xs ring-1 ring-red-500" style={{ backgroundColor: COLORS[2] }} />
          Has failures
        </span>
      </div>

      {/* Tooltip */}
      {tooltip && (
        <div
          className="pointer-events-none fixed z-50 rounded-sm bg-white px-3 py-2 text-xs/5 shadow-lg ring-1 ring-gray-200 dark:bg-gray-800 dark:text-gray-100 dark:ring-gray-700"
          style={{
            left: tooltip.x,
            top: tooltip.y - 8,
            transform: 'translate(-50%, -100%)',
          }}
        >
          <div className="flex flex-col gap-1">
            <div className="font-medium">Test #{tooltip.test.order}</div>
            {genesisMap.get(tooltip.test.testKey) && (
              <div className="text-gray-500 dark:text-gray-400">Genesis: {genesisMap.get(tooltip.test.testKey)}</div>
            )}
            <div>
              MGas/s:{' '}
              {tooltip.test.notProcessed
                ? 'Not processed yet'
                : tooltip.test.noData
                  ? 'No data'
                  : tooltip.test.mgasPerSec.toFixed(2)}
            </div>
            {!tooltip.test.noData && (
              <>
                <div>Gas used: {(tooltip.test.gasUsedTotal / 1_000_000).toFixed(2)} MGas</div>
                <div>Gas time: {formatDuration(tooltip.test.gasUsedTimeTotal)}</div>
              </>
            )}
            <div className="text-gray-500 dark:text-gray-400">Based on steps: {stepFilter.join(', ')}</div>
            <div className="w-48 break-all text-gray-500 dark:text-gray-400">{tooltip.test.filename}</div>
            {tooltip.test.notProcessed ? (
              <div className="text-gray-500 dark:text-gray-400">Test was not run</div>
            ) : tooltip.test.noData ? (
              <div className="text-gray-500 dark:text-gray-400">No gas usage data available</div>
            ) : null}
            {tooltip.test.hasFail && <div className="text-red-600 dark:text-red-400">Has failures</div>}
            {/* Only hint at clicking when the parent actually wired a
                handler — the live view doesn't, since there are no
                per-test details to open while the run is in progress. */}
            {onSelectedTestChange && (
              <div className="mt-1 text-gray-400 dark:text-gray-500">Click for details</div>
            )}
          </div>
        </div>
      )}

      {/* Test Detail Modal */}
      {selectedTest && tests[selectedTest] && (() => {
        const entry = tests[selectedTest]
        return (
          <Modal
            isOpen={!!selectedTest}
            onClose={() => onSelectedTestChange?.(undefined)}
            title={`Test #${executionOrder.get(selectedTest) ?? '?'}`}
          >
            <div className="flex flex-col gap-6">
              <div className="flex flex-col gap-2">
                <div>
                  <div className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Test Name</div>
                  <div className="flex items-center gap-2 text-sm/6 text-gray-900 dark:text-gray-100">
                    <span className="min-w-0 break-all">{selectedTest}</span>
                    <CopyButton text={selectedTest} />
                  </div>
                </div>
                {entry.dir && (
                  <div>
                    <div className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Directory</div>
                    <div className="flex items-center gap-2 text-sm/6 text-gray-900 dark:text-gray-100">
                      <span>{entry.dir}</span>
                      <CopyButton text={entry.dir} />
                    </div>
                  </div>
                )}
                {genesisMap.get(selectedTest) && (
                  <div>
                    <div className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Genesis</div>
                    <div className="flex items-center gap-2 text-sm/6 text-gray-900 dark:text-gray-100">
                      <span className="font-mono">{genesisMap.get(selectedTest)}</span>
                      <CopyButton text={genesisMap.get(selectedTest)!} />
                    </div>
                  </div>
                )}
              </div>
              {(() => {
                const matchingSuiteTest = suiteTests?.find((t) => t.name === selectedTest)
                if (!matchingSuiteTest) return null
                return <EESTInfoContent test={matchingSuiteTest} opcodeSort={opcodeSort} onOpcodeSortChange={setOpcodeSort} />
              })()}
              {(() => {
                const diffRows = opcodeDiffByTest?.[selectedTest]
                if (!diffRows || diffRows.length === 0) return null
                return (
                  <div className="flex flex-col gap-2">
                    <div className="flex items-center gap-2 text-xs/5 text-yellow-800 dark:text-yellow-200">
                      <span className="inline-flex items-center rounded-xs bg-yellow-100 px-1.5 py-0.5 font-medium dark:bg-yellow-900/50">
                        ⚠ Opcode counts differ from suite
                      </span>
                      <span className="text-gray-500 dark:text-gray-400">
                        {diffRows.length} opcode{diffRows.length === 1 ? '' : 's'} with non-zero Δ; sorted by absolute delta
                      </span>
                    </div>
                    <div className="max-h-72 overflow-y-auto rounded-xs border border-gray-200 bg-white p-2 dark:border-gray-700 dark:bg-gray-900">
                      <OpcodeDiffPanel rows={diffRows} />
                    </div>
                  </div>
                )
              })()}
              {blockLogs?.[selectedTest] && (
                <BlockLogDetails blockLog={blockLogs[selectedTest]} />
              )}
              {suiteHash && entry.steps && (() => {
                const steps = [
                  { key: 'test' as const, label: 'Test', step: entry.steps.test },
                  { key: 'setup' as const, label: 'Setup', step: entry.steps.setup },
                  { key: 'cleanup' as const, label: 'Cleanup', step: entry.steps.cleanup },
                ].filter(s => s.step)

                const activeStep = steps.find(s => s.key === activeStepTab) ?? steps[0]

                return (
                  <div className="flex flex-col gap-4">
                    {/* Step Tabs */}
                    <div className="flex gap-1 border-b border-gray-200 dark:border-gray-700">
                      {steps.map(({ key, label, step }) => {
                        const isActive = activeStep?.key === key
                        const success = step?.aggregated?.success ?? 0
                        const fail = step?.aggregated?.fail ?? 0
                        return (
                          <button
                            key={key}
                            onClick={() => setActiveStepTab(key)}
                            className={clsx(
                              'flex items-center gap-2 border-b-2 px-4 py-2 text-sm font-medium transition-colors',
                              isActive
                                ? 'border-blue-500 text-blue-600 dark:text-blue-400'
                                : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300',
                            )}
                          >
                            {label}
                            <span className="flex items-center gap-1">
                              {success > 0 && (
                                <span className="rounded-full bg-green-100 px-1.5 py-0.5 text-xs font-medium text-green-700 dark:bg-green-900/50 dark:text-green-300">
                                  {success}
                                </span>
                              )}
                              {fail > 0 && (
                                <span className="rounded-full bg-red-100 px-1.5 py-0.5 text-xs font-medium text-red-700 dark:bg-red-900/50 dark:text-red-300">
                                  {fail}
                                </span>
                              )}
                            </span>
                          </button>
                        )
                      })}
                    </div>

                    {/* Active Step Content */}
                    {activeStep && (
                      <div>
                        {activeStep.step?.aggregated && (
                          <div className="flex flex-col gap-4">
                            <TimeBreakdown methods={activeStep.step.aggregated.method_stats.times} />
                            <MGasBreakdown methods={activeStep.step.aggregated.method_stats.mgas_s} />
                          </div>
                        )}
                        <ExecutionsList
                          runId={runId}
                          suiteHash={suiteHash}
                          testName={selectedTest}
                          stepType={activeStep.key}
                          expandedRows={expandedExecRows}
                          onExpandedRowsChange={onExpandedExecRowsChange}
                        />
                      </div>
                    )}
                  </div>
                )
              })()}
              {postTestRPCCalls && postTestRPCCalls.length > 0 && (
                <PostTestDumps runId={runId} testName={selectedTest} calls={postTestRPCCalls} />
              )}
            </div>
          </Modal>
        )
      })()}
    </div>
  )
}
