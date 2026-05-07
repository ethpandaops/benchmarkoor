import { useCallback, useMemo, useState } from 'react'
import { Link, useParams, useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { fetchHead } from '@/api/client'
import type { TestEntry, AggregatedStats, StepResult } from '@/api/types'
import { useRunConfig } from '@/api/hooks/useRunConfig'
import { useRunResult } from '@/api/hooks/useRunResult'
import { useRunOpcodes } from '@/api/hooks/useRunOpcodes'
import { useSuite } from '@/api/hooks/useSuite'
import { RunConfiguration } from '@/components/run-detail/RunConfiguration'
import { MetadataLabels } from '@/components/run-detail/MetadataLabels'
import { GitHubSection } from '@/components/run-detail/GitHubSection'
import { FilesPanel } from '@/components/run-detail/FilesPanel'
import { ResourceUsageCharts } from '@/components/run-detail/ResourceUsageCharts'
import { TestsTable, type TestSortColumn, type TestSortDirection, type TestStatusFilter } from '@/components/run-detail/TestsTable'
import { PreRunStepsTable } from '@/components/run-detail/PreRunStepsTable'
import { TestHeatmap, type SortMode, type GroupMode } from '@/components/run-detail/TestHeatmap'
import { OpcodeHeatmap } from '@/components/suite-detail/OpcodeHeatmap'
import { OpcodeDiffPanel, type OpcodeDiffRow } from '@/components/run-detail/OpcodeDiffPanel'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { ClientStat } from '@/components/shared/ClientStat'
import { Duration } from '@/components/shared/Duration'
import { JDenticon } from '@/components/shared/JDenticon'
import { StatusAlert } from '@/components/shared/StatusBadge'
import { FilterInput } from '@/components/shared/FilterInput'
import { FacetPanel } from '@/components/shared/FacetPanel'
import { DimensionInsights } from '@/components/run-detail/DimensionInsights'
import { TEST_FILTER_HINT, toggleSearchTerm } from '@/utils/eestNameFilter'
import { formatTimestamp, formatDurationSeconds } from '@/utils/date'
import { formatNumber, formatBytes } from '@/utils/format'
import { useIndex, useLiveRuns } from '@/api/hooks/useIndex'
import { LiveRunDetailView } from '@/components/run-detail/LiveRunDetailView'
import { type IndexStepType, ALL_INDEX_STEP_TYPES } from '@/api/types'
import { ClientRunsStrip } from '@/components/run-detail/ClientRunsStrip'
import { BlockLogsDashboard } from '@/components/run-detail/block-logs-dashboard'
import { useBlockLogs } from '@/api/hooks/useBlockLogs'
import { Flame, Download, SquareStack, GitCompareArrows, Trash2 } from 'lucide-react'
import { MAX_COMPARE_RUNS, MIN_COMPARE_RUNS } from '@/components/compare/constants'
import { useAuth } from '@/hooks/useAuth'
import { useDeleteRuns } from '@/api/hooks/useAdmin'

// Per-test opcode diff between this run and the suite. Only opcodes
// where suite count != run count are listed.
interface OpcodeTestDiff {
  name: string
  rows: OpcodeDiffRow[]
}

// Step types that can be included in MGas/s calculation
export type StepTypeOption = 'setup' | 'test' | 'cleanup'
// eslint-disable-next-line react-refresh/only-export-components
export const ALL_STEP_TYPES: StepTypeOption[] = ['setup', 'test', 'cleanup']
// eslint-disable-next-line react-refresh/only-export-components
export const DEFAULT_STEP_FILTER: StepTypeOption[] = ['test']

// Aggregate stats from selected steps of a test entry
// eslint-disable-next-line react-refresh/only-export-components
export function getAggregatedStats(entry: TestEntry, stepFilter: StepTypeOption[] = ALL_STEP_TYPES): AggregatedStats | undefined {
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

  let timeTotal = 0
  let gasUsedTotal = 0
  let gasUsedTimeTotal = 0
  let success = 0
  let fail = 0
  let msgCount = 0
  const times: Record<string, { count: number; last: number }> = {}

  for (const step of steps) {
    if (step?.aggregated) {
      timeTotal += step.aggregated.time_total
      gasUsedTotal += step.aggregated.gas_used_total
      gasUsedTimeTotal += step.aggregated.gas_used_time_total
      success += step.aggregated.success
      fail += step.aggregated.fail
      msgCount += step.aggregated.msg_count

      for (const [method, stats] of Object.entries(step.aggregated.method_stats.times)) {
        if (!times[method]) {
          times[method] = { count: 0, last: 0 }
        }
        times[method].count += stats.count
        times[method].last = stats.last
      }
    }
  }

  return {
    time_total: timeTotal,
    gas_used_total: gasUsedTotal,
    gas_used_time_total: gasUsedTimeTotal,
    success,
    fail,
    msg_count: msgCount,
    method_stats: { times, mgas_s: {} },
  }
}

// Parse step filter from URL (comma-separated string) or use default
function parseStepFilter(param: string | undefined): StepTypeOption[] {
  if (!param) return DEFAULT_STEP_FILTER
  const steps = param.split(',').filter((s): s is StepTypeOption => ALL_STEP_TYPES.includes(s as StepTypeOption))
  return steps.length > 0 ? steps : DEFAULT_STEP_FILTER
}

// Serialize step filter to URL param (undefined if default)
function serializeStepFilter(steps: StepTypeOption[]): string | undefined {
  const sorted = [...steps].sort()
  const defaultSorted = [...DEFAULT_STEP_FILTER].sort()
  if (sorted.length === defaultSorted.length && sorted.every((s, i) => s === defaultSorted[i])) {
    return undefined
  }
  return steps.join(',')
}

export function RunDetailPage() {
  const { runId } = useParams({ from: '/runs/$runId' })
  const navigate = useNavigate()
  const { isAdmin } = useAuth()
  const deleteRuns = useDeleteRuns()
  const search = useSearch({ from: '/runs/$runId' }) as {
    page?: number
    pageSize?: number
    sortBy?: TestSortColumn
    sortDir?: TestSortDirection
    q?: string
    status?: TestStatusFilter
    testModal?: string
    preRunModal?: string
    heatmapSort?: SortMode
    heatmapGroup?: GroupMode
    heatmapThreshold?: number
    steps?: string
    ohFs?: boolean // Opcode Heatmap fullscreen
    blFs?: boolean // Block Logs fullscreen
    dlModal?: boolean // Download list modal
    dlFmt?: string // Download list format
    testStep?: string // Active step tab in test modal (test/setup/cleanup)
    testExec?: string // Expanded execution row indices (comma-separated)
  }
  const page = Number(search.page) || 1
  const pageSize = Number(search.pageSize) || 20
  const heatmapThreshold = search.heatmapThreshold ? Number(search.heatmapThreshold) : undefined
  const stepFilter = parseStepFilter(search.steps)
  const { sortBy = 'order', sortDir = 'asc', q = '', status = 'all', testModal, preRunModal, heatmapGroup, heatmapSort, ohFs = false, blFs = false, dlModal = false, dlFmt, testStep, testExec } = search
  const activeStepTab = (testStep === 'setup' || testStep === 'cleanup') ? testStep : testStep === 'test' ? testStep : undefined
  const expandedExecRows = testExec ? new Set(testExec.split(',').map(Number).filter(n => !isNaN(n))) : undefined

  const { data: liveRuns, isLoading: liveRunsLoading } = useLiveRuns()
  const liveRun = liveRuns?.find((lr) => lr.run_id === runId)

  // Skip fetching config/result on the on-disk backend while we know the
  // run is live — the files won't exist yet, and blocking the UI on three
  // retries of a 404'ing fetch delays the live view unnecessarily.
  const fetchOnDisk = !liveRun
  const { data: config, isLoading: configLoading, error: configError, refetch: refetchConfig } = useRunConfig(runId, fetchOnDisk)
  const { data: result, isLoading: resultLoading, refetch: refetchResult } = useRunResult(runId, fetchOnDisk)
  const { data: suite } = useSuite(config?.suite_hash ?? '')
  const { data: runOpcodes } = useRunOpcodes(runId, fetchOnDisk)
  const { data: index } = useIndex()
  const { data: containerLogHead, isLoading: containerLogLoading } = useQuery({
    queryKey: ['run', runId, 'container-log-head'],
    queryFn: () => fetchHead(`runs/${runId}/container.log`),
    enabled: !!runId,
  })
  const { data: benchmarkoorLogHead, isLoading: benchmarkoorLogLoading } = useQuery({
    queryKey: ['run', runId, 'benchmarkoor-log-head'],
    queryFn: () => fetchHead(`runs/${runId}/benchmarkoor.log`),
    enabled: !!runId,
  })
  const { data: blockLogs } = useBlockLogs(runId)

  const isLoading = liveRunsLoading || configLoading || resultLoading
  const error = configError

  const [compareMode, setCompareMode] = useState(false)
  const [selectedRunIds, setSelectedRunIds] = useState<Set<string>>(new Set())
  const [showOpcodeDiff, setShowOpcodeDiff] = useState(false)

  const handleSelectionChange = useCallback((id: string, selected: boolean) => {
    setSelectedRunIds((prev) => {
      const next = new Set(prev)
      if (selected) {
        if (next.size >= MAX_COMPARE_RUNS) return prev
        next.add(id)
      } else {
        next.delete(id)
      }
      return next
    })
  }, [])

  const handleExitCompareMode = useCallback(() => {
    setCompareMode(false)
    setSelectedRunIds(new Set())
  }, [])

  // Compute clientRuns and recentRuns before early returns to satisfy hooks rules.
  const clientRuns = useMemo(() => {
    if (!index || !config) return []
    return index.entries.filter(
      (r) => r.suite_hash === config.suite_hash && r.instance.client === config.instance.client,
    )
  }, [index, config])

  const recentRuns = useMemo(() => {
    const sorted = [...clientRuns].sort((a, b) => b.timestamp - a.timestamp)
    return sorted.slice(0, MAX_COMPARE_RUNS)
  }, [clientRuns])

  // Merge per-run opcode counts (test-opcodes.json) into the suite's
  // SuiteTest list so OpcodeHeatmap renders run-extracted data when
  // available. Per-test, sum counts across the array of newPayloads
  // (one entry per engine_newPayload* in the test step). If only the
  // suite has counts, the suite values are kept as-is.
  const { mergedSuiteTests, opcodeDiffs, opcodeDiffByTest } = useMemo(() => {
    if (!suite?.tests) {
      return {
        mergedSuiteTests: undefined,
        opcodeDiffs: [] as OpcodeTestDiff[],
        opcodeDiffByTest: {} as Record<string, OpcodeDiffRow[]>,
      }
    }

    const sumPayloads = (entries: Array<Record<string, number>>): Record<string, number> => {
      const out: Record<string, number> = {}
      for (const entry of entries) {
        for (const [op, count] of Object.entries(entry)) {
          out[op] = (out[op] ?? 0) + count
        }
      }
      return out
    }

    const diffOpcodes = (suiteCounts: Record<string, number>, runCounts: Record<string, number>): OpcodeDiffRow[] => {
      const all = new Set<string>([...Object.keys(suiteCounts), ...Object.keys(runCounts)])
      const rows: OpcodeDiffRow[] = []
      for (const op of all) {
        const s = suiteCounts[op] ?? 0
        const r = runCounts[op] ?? 0
        if (s !== r) rows.push({ opcode: op, suite: s, run: r, delta: r - s })
      }
      // Sort by largest absolute delta first.
      rows.sort((a, b) => Math.abs(b.delta) - Math.abs(a.delta))
      return rows
    }

    const diffs: OpcodeTestDiff[] = []
    const byTest: Record<string, OpcodeDiffRow[]> = {}
    const merged = suite.tests.map((t) => {
      const runEntries = runOpcodes?.[t.name]
      if (!runEntries || runEntries.length === 0) return t

      const runCounts = sumPayloads(runEntries)
      const suiteCounts = t.opcode_count ?? t.eest?.info?.opcode_count
      if (suiteCounts && Object.keys(suiteCounts).length > 0) {
        const rows = diffOpcodes(suiteCounts, runCounts)
        if (rows.length > 0) {
          diffs.push({ name: t.name, rows })
          byTest[t.name] = rows
        }
      }
      return { ...t, opcode_count: runCounts }
    })

    return { mergedSuiteTests: merged, opcodeDiffs: diffs, opcodeDiffByTest: byTest }
  }, [suite, runOpcodes])

  const updateSearch = (updates: Partial<typeof search>) => {
    navigate({
      to: '/runs/$runId',
      params: { runId },
      search: {
        ...search, // Preserve all existing params (including block logs bl* params)
        page,
        pageSize,
        sortBy,
        sortDir,
        q: q || undefined,
        status: status !== 'all' ? status : undefined,
        testModal,
        preRunModal,
        heatmapSort,
        heatmapThreshold,
        steps: serializeStepFilter(stepFilter),
        ohFs: ohFs || undefined,
        blFs: blFs || undefined,
        dlModal: dlModal || undefined,
        dlFmt: dlFmt || undefined,
        ...updates,
      },
    })
  }

  const handlePageChange = (newPage: number) => {
    updateSearch({ page: newPage })
  }

  const handlePageSizeChange = (newSize: number) => {
    updateSearch({ pageSize: newSize, page: 1 })
  }

  const handleSortChange = (column: TestSortColumn, direction: TestSortDirection) => {
    updateSearch({ sortBy: column, sortDir: direction })
  }

  const handleSearchChange = (query: string) => {
    updateSearch({ q: query || undefined, page: 1 })
  }

  const handleStatusFilterChange = (newStatus: TestStatusFilter) => {
    updateSearch({ status: newStatus !== 'all' ? newStatus : undefined, page: 1 })
  }

  const handleTestModalChange = (testName: string | undefined) => {
    // Clear step tab and expanded rows when closing or switching test
    updateSearch({ testModal: testName, testStep: undefined, testExec: undefined })
  }

  const handleStepTabChange = (tab: 'test' | 'setup' | 'cleanup') => {
    // Clear expanded rows when switching tabs
    updateSearch({ testStep: tab !== 'test' ? tab : undefined, testExec: undefined })
  }

  const handleExpandedExecRowsChange = (rows: Set<number>) => {
    updateSearch({ testExec: rows.size > 0 ? [...rows].sort((a, b) => a - b).join(',') : undefined })
  }

  const handlePreRunModalChange = (stepName: string | undefined) => {
    updateSearch({ preRunModal: stepName })
  }

  const handleHeatmapSortChange = (mode: SortMode) => {
    updateSearch({ heatmapSort: mode !== 'order' ? mode : undefined })
  }

  const handleHeatmapGroupChange = (mode: GroupMode) => {
    updateSearch({ heatmapGroup: mode !== 'none' ? mode : undefined })
  }

  const handleHeatmapThresholdChange = (threshold: number) => {
    updateSearch({ heatmapThreshold: threshold !== 60 ? threshold : undefined })
  }

  const handleStepFilterChange = (steps: StepTypeOption[]) => {
    updateSearch({ steps: serializeStepFilter(steps) })
  }

  const handleOpcodeHeatmapFullscreenChange = (fullscreen: boolean) => {
    updateSearch({ ohFs: fullscreen || undefined })
  }

  const handleBlockLogsFullscreenChange = (fullscreen: boolean) => {
    updateSearch({ blFs: fullscreen || undefined })
  }

  const handleDownloadListModalChange = (open: boolean) => {
    updateSearch({ dlModal: open || undefined })
  }

  const handleDownloadFormatChange = (format: string) => {
    updateSearch({ dlFmt: format !== 'curl' ? format : undefined })
  }

  // Short-circuit: if the ingest API is reporting this run as live and
  // we don't have a completed config.json yet, render the live view
  // immediately. This avoids waiting for on-disk config.json retries,
  // which would block the UI for several seconds while react-query
  // exhausts its retry budget against a 404.
  if (liveRun && !config) {
    return <LiveRunDetailView run={liveRun} />
  }

  if (isLoading) {
    return <LoadingState message="Loading run details..." />
  }

  if (error) {
    return (
      <ErrorState
        message={error.message}
        retry={() => {
          refetchConfig()
          refetchResult()
        }}
      />
    )
  }

  if (!config) {
    return <ErrorState message="Run not found" />
  }

  // Map StepTypeOption[] to IndexStepType[] for the strip
  const indexStepFilter: IndexStepType[] = stepFilter.filter(
    (s): s is IndexStepType => ALL_INDEX_STEP_TYPES.includes(s as IndexStepType),
  )

  // Compute result-dependent stats only when result.json is available.
  const aggregatedStats = result
    ? Object.values(result.tests).map((t) => getAggregatedStats(t, stepFilter)).filter((s): s is AggregatedStats => s !== undefined)
    : []
  const testCount = config.test_counts?.total ?? (result ? Object.keys(result.tests).length : 0)
  const passedTests = config.test_counts?.passed ?? aggregatedStats.filter((s) => s.fail === 0).length
  const failedTests = config.test_counts ? (config.test_counts.total - config.test_counts.passed) : aggregatedStats.filter((s) => s.fail > 0).length
  const totalDuration = aggregatedStats.reduce((sum, s) => sum + s.time_total, 0)
  const totalGasUsed = aggregatedStats.reduce((sum, s) => sum + s.gas_used_total, 0)
  const totalGasUsedTime = aggregatedStats.reduce((sum, s) => sum + s.gas_used_time_total, 0)
  const mgasPerSec = totalGasUsedTime > 0 ? (totalGasUsed * 1000) / totalGasUsedTime : undefined
  const totalMsgCount = aggregatedStats.reduce((sum, s) => sum + s.msg_count, 0)
  const methodCounts = aggregatedStats.reduce<Record<string, number>>((acc, s) => {
    Object.entries(s.method_stats.times).forEach(([method, stats]) => {
      acc[method] = (acc[method] ?? 0) + stats.count
    })
    return acc
  }, {})

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm/6 text-gray-500 dark:text-gray-400">
        <div className="flex min-w-0 items-center gap-2">
          <Link to="/suites" className="shrink-0 hover:text-gray-700 dark:hover:text-gray-300">
            Suites
          </Link>
          <span>/</span>
          {config.suite_hash && (
            <>
              <Link
                to="/suites/$suiteHash"
                params={{ suiteHash: config.suite_hash }}
                className={`flex min-w-0 items-center gap-1.5 hover:text-gray-700 dark:hover:text-gray-300${suite?.metadata?.labels?.name ? '' : ' font-mono'}`}
              >
                <JDenticon value={config.suite_hash} size={16} className="shrink-0 rounded-xs" />
                <span className="truncate">{suite?.metadata?.labels?.name ?? config.suite_hash}</span>
              </Link>
              <span>/</span>
            </>
          )}
          <span className="truncate text-gray-900 dark:text-gray-100">{runId}</span>
          {isAdmin && (
            <button
              disabled={deleteRuns.isPending}
              onClick={() => {
                if (!window.confirm('Delete this run? This cannot be undone.')) return
                deleteRuns.mutate([runId], {
                  onSuccess: () => navigate({ to: '/runs' }),
                })
              }}
              className="ml-1 flex shrink-0 items-center justify-center rounded-xs p-1 text-gray-400 transition-colors hover:bg-red-50 hover:text-red-600 disabled:opacity-50 dark:text-gray-500 dark:hover:bg-red-900/20 dark:hover:text-red-400"
              title="Delete this run"
            >
              <Trash2 className="size-3.5" />
            </button>
          )}
        </div>
        {(benchmarkoorLogLoading || benchmarkoorLogHead?.exists || containerLogLoading || containerLogHead?.exists) && (
          <div className="flex items-center gap-2 sm:ml-auto">
            <span className="font-medium text-gray-900 dark:text-gray-100">Logs:</span>
            {(benchmarkoorLogLoading || benchmarkoorLogHead?.exists) && (
              <>
                <Link
                  to="/runs/$runId/fileviewer"
                  params={{ runId }}
                  search={{ file: 'benchmarkoor.log' }}
                  target="_blank"
                  className="hover:text-gray-700 dark:hover:text-gray-300"
                >
                  Benchmarkoor
                </Link>
                <span className="text-xs text-gray-400 dark:text-gray-500">
                  {benchmarkoorLogLoading ? (
                    <span className="inline-block size-3 animate-pulse rounded-full bg-gray-200 dark:bg-gray-600" />
                  ) : benchmarkoorLogHead?.size != null ? (
                    `(${formatBytes(benchmarkoorLogHead.size)})`
                  ) : null}
                </span>
                <a
                  href={benchmarkoorLogHead?.url}
                  download="benchmarkoor.log"
                  className="text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                  title="Download benchmarkoor.log"
                >
                  <Download className="size-4" />
                </a>
              </>
            )}
            {(benchmarkoorLogLoading || benchmarkoorLogHead?.exists) && (containerLogLoading || containerLogHead?.exists) && (
              <span className="text-gray-300 dark:text-gray-600">|</span>
            )}
            {(containerLogLoading || containerLogHead?.exists) && (
              <>
                <Link
                  to="/runs/$runId/fileviewer"
                  params={{ runId }}
                  search={{ file: 'container.log' }}
                  target="_blank"
                  className="hover:text-gray-700 dark:hover:text-gray-300"
                >
                  Client
                </Link>
                <span className="text-xs text-gray-400 dark:text-gray-500">
                  {containerLogLoading ? (
                    <span className="inline-block size-3 animate-pulse rounded-full bg-gray-200 dark:bg-gray-600" />
                  ) : containerLogHead?.size != null ? (
                    `(${formatBytes(containerLogHead.size)})`
                  ) : null}
                </span>
                <a
                  href={containerLogHead?.url}
                  download="container.log"
                  className="text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                  title="Download container.log"
                >
                  <Download className="size-4" />
                </a>
              </>
            )}
          </div>
        )}
      </div>

      {clientRuns.length > 1 && (
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <div className="min-w-0 flex-1">
            <ClientRunsStrip runs={clientRuns} currentRunId={runId} stepFilter={indexStepFilter} selectable={compareMode} selectedRunIds={selectedRunIds} onSelectionChange={handleSelectionChange} />
          </div>
          <div className="flex shrink-0 items-center gap-1.5">
            <button
              onClick={() => compareMode ? handleExitCompareMode() : setCompareMode(true)}
              className={`flex cursor-pointer items-center justify-center rounded-xs p-1.5 shadow-xs ring-1 ring-inset transition-colors ${
                compareMode
                  ? 'bg-blue-600 text-white ring-blue-600 hover:bg-blue-700 hover:ring-blue-700'
                  : 'bg-white text-gray-500 ring-gray-300 hover:bg-gray-50 hover:text-gray-700 dark:bg-gray-800 dark:text-gray-400 dark:ring-gray-600 dark:hover:bg-gray-700 dark:hover:text-gray-200'
              }`}
              title="Compare"
            >
              <SquareStack className="size-4" />
            </button>
            <button
              disabled={recentRuns.length < MIN_COMPARE_RUNS}
              onClick={() => {
                const ids = recentRuns.map((r) => r.run_id)
                navigate({ to: '/compare', search: { runs: ids.join(',') } })
              }}
              className="flex cursor-pointer items-center justify-center rounded-xs p-1.5 shadow-xs ring-1 ring-inset transition-colors bg-white text-gray-500 ring-gray-300 hover:bg-gray-50 hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-gray-800 dark:text-gray-400 dark:ring-gray-600 dark:hover:bg-gray-700 dark:hover:text-gray-200"
              title={`Compare last ${recentRuns.length} runs`}
            >
              <GitCompareArrows className="size-4" />
            </button>
          </div>
        </div>
      )}

      <StatusAlert
        status={config.status}
        terminationReason={config.termination_reason}
        containerExitCode={config.container_exit_code}
        containerOOMKilled={config.container_oom_killed}
      />

      <div className="grid grid-cols-2 gap-4 sm:grid-cols-5">
        <ClientStat client={config.instance.client} runId={config.instance.id} rollbackStrategy={config.instance.rollback_strategy} />
        {(config.test_counts || result) ? (
          <>
            <div className="rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
              <p className="text-sm/6 font-medium text-gray-500 dark:text-gray-400">Tests</p>
              <p className="mt-1 flex items-center gap-2 text-2xl/8 font-semibold">
                <span className="text-gray-900 dark:text-gray-100">{testCount}</span>
                <span className="text-gray-400 dark:text-gray-500">/</span>
                <span className="text-green-600 dark:text-green-400">{passedTests}</span>
                {failedTests > 0 && (
                  <>
                    <span className="text-gray-400 dark:text-gray-500">/</span>
                    <span className="text-red-600 dark:text-red-400">{failedTests}</span>
                  </>
                )}
              </p>
              <p className="mt-2 text-xs/5 text-gray-500 dark:text-gray-400">
                Started at
              </p>
              <p className="text-xs/5 text-gray-900 dark:text-gray-100">
                {formatTimestamp(config.timestamp)}
              </p>
            </div>
            {result ? (
              <>
                <div className="rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
                  <div className="flex items-center justify-between">
                    <p className="text-sm/6 font-medium text-gray-500 dark:text-gray-400">MGas/s</p>
                    <div className="flex items-center gap-1">
                      {ALL_STEP_TYPES.map((step) => (
                        <button
                          key={step}
                          onClick={() => {
                            const newFilter = stepFilter.includes(step)
                              ? stepFilter.filter((s) => s !== step)
                              : [...stepFilter, step]
                            if (newFilter.length > 0) {
                              handleStepFilterChange(newFilter)
                            }
                          }}
                          className={`rounded-xs px-1.5 py-0.5 text-xs font-medium transition-colors ${
                            stepFilter.includes(step)
                              ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300'
                              : 'bg-gray-100 text-gray-400 dark:bg-gray-700 dark:text-gray-500'
                          }`}
                          title={`${stepFilter.includes(step) ? 'Exclude' : 'Include'} ${step} step in MGas/s calculation`}
                        >
                          {step.charAt(0).toUpperCase()}
                        </button>
                      ))}
                    </div>
                  </div>
                  <p className="mt-1 text-2xl/8 font-semibold text-gray-900 dark:text-gray-100">
                    {mgasPerSec !== undefined ? mgasPerSec.toFixed(2) : '-'}
                  </p>
                  <p className="mt-2 text-xs/5 text-gray-500 dark:text-gray-400">
                    <span title={`${formatNumber(totalGasUsed)} gas`}>
                      {totalGasUsed >= 1_000_000_000
                        ? `${(totalGasUsed / 1_000_000_000).toFixed(2)} GGas`
                        : `${(totalGasUsed / 1_000_000).toFixed(2)} MGas`}
                    </span>
                    {' '}in <Duration nanoseconds={totalGasUsedTime} />
                  </p>
                </div>
                <div className="rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
                  <p className="text-sm/6 font-medium text-gray-500 dark:text-gray-400">Calls</p>
                  <p className="mt-1 text-2xl/8 font-semibold text-gray-900 dark:text-gray-100">
                    {formatNumber(totalMsgCount)}
                  </p>
                  {Object.keys(methodCounts).length > 0 && (
                    <div className="mt-2 flex flex-col gap-0.5 text-xs/5 text-gray-500 dark:text-gray-400">
                      {Object.entries(methodCounts)
                        .sort(([, a], [, b]) => b - a)
                        .map(([method, count]) => (
                          <div key={method} className="flex justify-between gap-2">
                            <span>{method}</span>
                            <span>{formatNumber(count)}</span>
                          </div>
                        ))}
                    </div>
                  )}
                </div>
                <div className="rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
                  <p className="text-sm/6 font-medium text-gray-500 dark:text-gray-400">Test Duration</p>
                  <p className="mt-1 text-2xl/8 font-semibold text-gray-900 dark:text-gray-100">
                    <Duration nanoseconds={totalDuration} />
                  </p>
                  {config.timestamp_end != null && config.timestamp_end > 0 && (
                    <>
                      <p className="mt-2 text-xs/5 text-gray-500 dark:text-gray-400">
                        Total runtime
                      </p>
                      <p className="text-xs/5 text-gray-900 dark:text-gray-100">
                        {formatDurationSeconds(config.timestamp_end - config.timestamp)}
                      </p>
                    </>
                  )}
                </div>
              </>
            ) : (
              <div className="col-span-3 rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
                <p className="text-sm/6 font-medium text-gray-500 dark:text-gray-400">Results</p>
                <p className="mt-1 text-sm/6 text-gray-500 dark:text-gray-400">
                  No result.json available. The run may still be in progress or may have failed before producing results.
                </p>
                {config.timestamp_end != null && config.timestamp_end > 0 && (
                  <>
                    <p className="mt-2 text-xs/5 text-gray-500 dark:text-gray-400">
                      Total runtime
                    </p>
                    <p className="text-xs/5 text-gray-900 dark:text-gray-100">
                      {formatDurationSeconds(config.timestamp_end - config.timestamp)}
                    </p>
                  </>
                )}
              </div>
            )}
          </>
        ) : (
          <div className="col-span-4 rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
            <p className="text-sm/6 font-medium text-gray-500 dark:text-gray-400">Results</p>
            <p className="mt-1 text-sm/6 text-gray-500 dark:text-gray-400">
              No result.json available. The run may still be in progress or may have failed before producing results.
            </p>
            <p className="mt-2 text-xs/5 text-gray-500 dark:text-gray-400">
              Started at
            </p>
            <p className="text-xs/5 text-gray-900 dark:text-gray-100">
              {formatTimestamp(config.timestamp)}
            </p>
            {config.timestamp_end != null && config.timestamp_end > 0 && (
              <>
                <p className="mt-1 text-xs/5 text-gray-500 dark:text-gray-400">
                  Total runtime
                </p>
                <p className="text-xs/5 text-gray-900 dark:text-gray-100">
                  {formatDurationSeconds(config.timestamp_end - config.timestamp)}
                </p>
              </>
            )}
          </div>
        )}
      </div>

      <MetadataLabels labels={config.metadata?.labels} />

      <GitHubSection labels={config.metadata?.labels} />

      <RunConfiguration instance={config.instance} system={config.system} startBlock={config.start_block} metadata={config.metadata} benchmarkoorVersion={config.benchmarkoor_version} />

      <FilesPanel
        runId={runId}
        tests={result?.tests ?? {}}
        postTestRPCCalls={config.instance.post_test_rpc_calls}
        showDownloadList={dlModal}
        downloadFormat={(dlFmt as 'urls' | 'curl') ?? 'curl'}
        onShowDownloadListChange={handleDownloadListModalChange}
        onDownloadFormatChange={handleDownloadFormatChange}
      />

      {result && (
        <>
          <FacetPanel
            testNames={Object.keys(result.tests)}
            query={q}
            onToggle={(term) => handleSearchChange(toggleSearchTerm(q, term))}
          />
          <DimensionInsights
            tests={result.tests}
            stepFilter={stepFilter}
            searchQuery={q}
            statusFilter={status}
            query={q}
            onToggle={(term) => handleSearchChange(toggleSearchTerm(q, term))}
          />
          <div className="overflow-hidden rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
            <div className="mb-4 flex items-center justify-between">
              <h3 className="flex items-center gap-2 text-sm/6 font-medium text-gray-900 dark:text-gray-100">
                <Flame className="size-4 text-gray-400 dark:text-gray-500" />
                Performance Heatmap
              </h3>
              <FilterInput
                placeholder="Search… or e.g. opcode:ORIGIN gas:90M"
                title={TEST_FILTER_HINT}
                value={q}
                onValueChange={handleSearchChange}
                className="w-72 rounded-xs border border-gray-300 bg-white px-3 py-1 text-sm/6 placeholder-gray-400 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 dark:placeholder-gray-500"
              />
            </div>
            <TestHeatmap
              tests={result.tests}
              suiteTests={mergedSuiteTests ?? suite?.tests}
              opcodeDiffByTest={opcodeDiffByTest}
              runId={runId}
              suiteHash={config.suite_hash}
              selectedTest={testModal}
              statusFilter={status}
              searchQuery={q}
              sortMode={heatmapSort}
              threshold={heatmapThreshold}
              stepFilter={stepFilter}
              postTestRPCCalls={config.instance.post_test_rpc_calls}
              onSelectedTestChange={handleTestModalChange}
              onSortModeChange={handleHeatmapSortChange}
              groupMode={heatmapGroup}
              onGroupModeChange={handleHeatmapGroupChange}
              onThresholdChange={handleHeatmapThresholdChange}
              onSearchChange={handleSearchChange}
              activeStepTab={activeStepTab}
              onActiveStepTabChange={handleStepTabChange}
              expandedExecRows={expandedExecRows}
              onExpandedExecRowsChange={handleExpandedExecRowsChange}
            />
          </div>

          {mergedSuiteTests && mergedSuiteTests.length > 0 && (
            <div className="overflow-hidden rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
              {runOpcodes && Object.keys(runOpcodes).length > 0 && (
                <div className="mb-3 rounded-sm border border-blue-200 bg-blue-50 px-3 py-2 text-xs/5 text-blue-800 dark:border-blue-900/50 dark:bg-blue-950/40 dark:text-blue-200">
                  <div className="flex flex-wrap items-center gap-2">
                    <span>
                      Showing opcode counts extracted from this run (
                      <code className="font-mono">test-opcodes.json</code>); suite-defined counts are
                      overridden where available.
                    </span>
                    {opcodeDiffs.length > 0 && (
                      <button
                        type="button"
                        onClick={() => setShowOpcodeDiff((v) => !v)}
                        className="inline-flex items-center rounded-xs bg-yellow-100 px-1.5 py-0.5 font-medium text-yellow-800 hover:bg-yellow-200 dark:bg-yellow-900/50 dark:text-yellow-200 dark:hover:bg-yellow-900/70"
                      >
                        ⚠ {opcodeDiffs.length} test{opcodeDiffs.length === 1 ? '' : 's'} differ from suite
                        <span className="ml-1.5 text-yellow-700 dark:text-yellow-300">{showOpcodeDiff ? '▾' : '▸'}</span>
                      </button>
                    )}
                  </div>
                  {showOpcodeDiff && opcodeDiffs.length > 0 && (
                    <div className="mt-3 max-h-96 overflow-y-auto rounded-xs border border-blue-200 bg-white p-2 text-gray-900 dark:border-blue-900/50 dark:bg-gray-900 dark:text-gray-100">
                      <div className="flex flex-col gap-3">
                        {opcodeDiffs.map((d) => (
                          <OpcodeDiffPanel key={d.name} caption={d.name} rows={d.rows} />
                        ))}
                      </div>
                    </div>
                  )}
                </div>
              )}
              <OpcodeHeatmap
                tests={mergedSuiteTests}
                extraColumns={[{
                  name: 'Mgas/s',
                  getValue: (testIndex: number) => {
                    const testName = mergedSuiteTests[testIndex]?.name
                    if (!testName) return undefined
                    const entry = result.tests[testName]
                    if (!entry) return undefined
                    const stats = getAggregatedStats(entry, stepFilter)
                    if (!stats || stats.gas_used_time_total <= 0) return undefined
                    return (stats.gas_used_total * 1000) / stats.gas_used_time_total
                  },
                  width: 54,
                  format: (v: number) => v.toFixed(1),
                }]}
                onTestClick={(testIndex) => handleTestModalChange(mergedSuiteTests[testIndex - 1]?.name)}
                searchQuery={q}
                onSearchChange={handleSearchChange}
                fullscreen={ohFs}
                onFullscreenChange={handleOpcodeHeatmapFullscreenChange}
              />
            </div>
          )}

          {blockLogs && Object.keys(blockLogs).length > 0 && (
            <BlockLogsDashboard blockLogs={blockLogs} runId={runId} suiteTests={suite?.tests} onTestClick={handleTestModalChange} searchQuery={q} onSearchChange={handleSearchChange} fullscreen={blFs} onFullscreenChange={handleBlockLogsFullscreenChange} />
          )}

          <ResourceUsageCharts
            tests={result.tests}
            suiteTests={mergedSuiteTests ?? suite?.tests}
            searchQuery={q}
            statusFilter={status}
            onTestClick={handleTestModalChange}
            resourceCollectionMethod={config.system_resource_collection_method}
            cpuCores={config.instance.resource_limits?.cpuset_cpus
              ? config.instance.resource_limits.cpuset_cpus.split(',').length
              : config.system.cpu_cores}
          />

          {result.pre_run_steps && Object.keys(result.pre_run_steps).length > 0 && (
            <PreRunStepsTable
              preRunSteps={result.pre_run_steps}
              suitePreRunSteps={suite?.pre_run_steps}
              runId={runId}
              suiteHash={config.suite_hash}
              selectedStep={preRunModal}
              onSelectedStepChange={handlePreRunModalChange}
            />
          )}

          <TestsTable
            tests={result.tests}
            suiteTests={suite?.tests}
            currentPage={page}
            pageSize={pageSize}
            sortBy={sortBy}
            sortDir={sortDir}
            searchQuery={q}
            statusFilter={status}
            stepFilter={stepFilter}
            onPageChange={handlePageChange}
            onPageSizeChange={handlePageSizeChange}
            onSortChange={handleSortChange}
            onSearchChange={handleSearchChange}
            onStatusFilterChange={handleStatusFilterChange}
            onTestClick={handleTestModalChange}
          />
        </>
      )}

      {compareMode && (
        <div className="fixed inset-x-0 bottom-0 z-50 border-t border-gray-200 bg-white px-6 py-3 shadow-sm dark:border-gray-700 dark:bg-gray-800">
          <div className="mx-auto flex max-w-7xl items-center justify-between">
            <span className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">
              {selectedRunIds.size} of {MAX_COMPARE_RUNS} selected
            </span>
            <div className="flex items-center gap-2">
              <button
                onClick={handleExitCompareMode}
                className="rounded-sm px-3 py-1.5 text-sm/6 font-medium text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-700"
              >
                Cancel
              </button>
              <button
                disabled={selectedRunIds.size < MIN_COMPARE_RUNS}
                onClick={() => {
                  const ids = Array.from(selectedRunIds)
                  navigate({ to: '/compare', search: { runs: ids.join(',') } })
                }}
                className="rounded-sm bg-blue-600 px-4 py-1.5 text-sm/6 font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
              >
                Compare
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
