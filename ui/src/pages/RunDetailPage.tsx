import { Link, useParams, useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { fetchText } from '@/api/client'
import type { TestEntry, AggregatedStats, StepResult } from '@/api/types'
import { useRunConfig } from '@/api/hooks/useRunConfig'
import { useRunResult } from '@/api/hooks/useRunResult'
import { useSuite } from '@/api/hooks/useSuite'
import { RunConfiguration } from '@/components/run-detail/RunConfiguration'
import { ResourceUsageCharts } from '@/components/run-detail/ResourceUsageCharts'
import { TestsTable, type TestSortColumn, type TestSortDirection, type TestStatusFilter } from '@/components/run-detail/TestsTable'
import { PreRunStepsTable } from '@/components/run-detail/PreRunStepsTable'
import { TestHeatmap, type SortMode } from '@/components/run-detail/TestHeatmap'
import { OpcodeHeatmap } from '@/components/suite-detail/OpcodeHeatmap'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { ClientStat } from '@/components/shared/ClientStat'
import { Duration } from '@/components/shared/Duration'
import { JDenticon } from '@/components/shared/JDenticon'
import { StatusAlert } from '@/components/shared/StatusBadge'
import { formatTimestamp } from '@/utils/date'
import { formatNumber, formatBytes } from '@/utils/format'
import { useIndex } from '@/api/hooks/useIndex'
import { type IndexStepType, ALL_INDEX_STEP_TYPES } from '@/api/types'
import { ClientRunsStrip } from '@/components/run-detail/ClientRunsStrip'

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
  const search = useSearch({ from: '/runs/$runId' }) as {
    page?: number
    pageSize?: number
    sortBy?: TestSortColumn
    sortDir?: TestSortDirection
    q?: string
    status?: TestStatusFilter
    testModal?: string
    heatmapSort?: SortMode
    heatmapThreshold?: number
    steps?: string
  }
  const page = Number(search.page) || 1
  const pageSize = Number(search.pageSize) || 20
  const heatmapThreshold = search.heatmapThreshold ? Number(search.heatmapThreshold) : undefined
  const stepFilter = parseStepFilter(search.steps)
  const { sortBy = 'order', sortDir = 'asc', q = '', status = 'all', testModal, heatmapSort } = search

  const { data: config, isLoading: configLoading, error: configError, refetch: refetchConfig } = useRunConfig(runId)
  const { data: result, isLoading: resultLoading, error: resultError, refetch: refetchResult } = useRunResult(runId)
  const { data: suite } = useSuite(config?.suite_hash ?? '')
  const { data: index } = useIndex()
  const { data: containerLog } = useQuery({
    queryKey: ['run', runId, 'container-log'],
    queryFn: async () => {
      const { data } = await fetchText(`runs/${runId}/container.log`)
      return data
    },
    enabled: !!runId,
  })
  const { data: benchmarkoorLog } = useQuery({
    queryKey: ['run', runId, 'benchmarkoor-log'],
    queryFn: async () => {
      const { data } = await fetchText(`runs/${runId}/benchmarkoor.log`)
      return data
    },
    enabled: !!runId,
  })

  const isLoading = configLoading || resultLoading
  const error = configError || resultError

  const updateSearch = (updates: Partial<typeof search>) => {
    navigate({
      to: '/runs/$runId',
      params: { runId },
      search: {
        page,
        pageSize,
        sortBy,
        sortDir,
        q: q || undefined,
        status: status !== 'all' ? status : undefined,
        testModal,
        heatmapSort,
        heatmapThreshold,
        steps: serializeStepFilter(stepFilter),
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
    updateSearch({ testModal: testName })
  }

  const handleHeatmapSortChange = (mode: SortMode) => {
    updateSearch({ heatmapSort: mode !== 'order' ? mode : undefined })
  }

  const handleHeatmapThresholdChange = (threshold: number) => {
    updateSearch({ heatmapThreshold: threshold !== 60 ? threshold : undefined })
  }

  const handleStepFilterChange = (steps: StepTypeOption[]) => {
    updateSearch({ steps: serializeStepFilter(steps) })
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

  if (!config || !result) {
    return <ErrorState message="Run not found" />
  }

  const clientRuns = (index?.entries ?? []).filter(
    (r) => r.suite_hash === config.suite_hash && r.instance.client === config.instance.client,
  )

  // Map StepTypeOption[] to IndexStepType[] for the strip
  const indexStepFilter: IndexStepType[] = stepFilter.filter(
    (s): s is IndexStepType => ALL_INDEX_STEP_TYPES.includes(s as IndexStepType),
  )

  const testCount = Object.keys(result.tests).length
  const aggregatedStats = Object.values(result.tests).map((t) => getAggregatedStats(t, stepFilter)).filter((s): s is AggregatedStats => s !== undefined)
  const passedTests = aggregatedStats.filter((s) => s.fail === 0).length
  const failedTests = aggregatedStats.filter((s) => s.fail > 0).length
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
      <div className="flex items-center gap-2 text-sm/6 text-gray-500 dark:text-gray-400">
        <Link to="/suites" className="hover:text-gray-700 dark:hover:text-gray-300">
          Suites
        </Link>
        <span>/</span>
        {config.suite_hash && (
          <>
            <Link
              to="/suites/$suiteHash"
              params={{ suiteHash: config.suite_hash }}
              className="flex items-center gap-1.5 font-mono hover:text-gray-700 dark:hover:text-gray-300"
            >
              <JDenticon value={config.suite_hash} size={16} className="shrink-0 rounded-xs" />
              {config.suite_hash}
            </Link>
            <span>/</span>
          </>
        )}
        <span className="text-gray-900 dark:text-gray-100">{runId}</span>
        {(containerLog || benchmarkoorLog) && (
          <div className="ml-auto flex items-center gap-2">
            <span className="font-medium text-gray-900 dark:text-gray-100">Logs:</span>
            {benchmarkoorLog && (
              <>
                <Link
                  to="/runs/$runId/logs"
                  params={{ runId }}
                  search={{ file: 'benchmarkoor.log' }}
                  className="hover:text-gray-700 dark:hover:text-gray-300"
                >
                  Benchmarkoor
                </Link>
                <span className="text-xs text-gray-400 dark:text-gray-500">
                  ({formatBytes(new TextEncoder().encode(benchmarkoorLog).length)})
                </span>
                <button
                  onClick={() => {
                    const blob = new Blob([benchmarkoorLog], { type: 'text/plain' })
                    const url = URL.createObjectURL(blob)
                    const a = document.createElement('a')
                    a.href = url
                    a.download = 'benchmarkoor.log'
                    document.body.appendChild(a)
                    a.click()
                    document.body.removeChild(a)
                    URL.revokeObjectURL(url)
                  }}
                  className="text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                  title="Download benchmarkoor.log"
                >
                  <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={2}
                      d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"
                    />
                  </svg>
                </button>
              </>
            )}
            {benchmarkoorLog && containerLog && <span className="text-gray-300 dark:text-gray-600">|</span>}
            {containerLog && (
              <>
                <Link
                  to="/runs/$runId/logs"
                  params={{ runId }}
                  search={{ file: 'container.log' }}
                  className="hover:text-gray-700 dark:hover:text-gray-300"
                >
                  Client
                </Link>
                <span className="text-xs text-gray-400 dark:text-gray-500">
                  ({formatBytes(new TextEncoder().encode(containerLog).length)})
                </span>
                <button
                  onClick={() => {
                    const blob = new Blob([containerLog], { type: 'text/plain' })
                    const url = URL.createObjectURL(blob)
                    const a = document.createElement('a')
                    a.href = url
                    a.download = 'container.log'
                    document.body.appendChild(a)
                    a.click()
                    document.body.removeChild(a)
                    URL.revokeObjectURL(url)
                  }}
                  className="text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
                  title="Download container.log"
                >
                  <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                    <path
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      strokeWidth={2}
                      d="M4 16v1a3 3 0 003 3h10a3 3 0 003-3v-1m-4-4l-4 4m0 0l-4-4m4 4V4"
                    />
                  </svg>
                </button>
              </>
            )}
          </div>
        )}
      </div>

      {clientRuns.length > 1 && (
        <ClientRunsStrip runs={clientRuns} currentRunId={runId} stepFilter={indexStepFilter} />
      )}

      <StatusAlert
        status={config.status}
        terminationReason={config.termination_reason}
        containerExitCode={config.container_exit_code}
      />

      <div className="grid grid-cols-2 gap-4 sm:grid-cols-5">
        <ClientStat client={config.instance.client} runId={config.instance.id} />
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
        </div>
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
          <p className="mt-2 text-xs/5 text-gray-500 dark:text-gray-400">
            Started at
          </p>
          <p className="text-xs/5 text-gray-900 dark:text-gray-100">
            {formatTimestamp(config.timestamp)}
          </p>
        </div>
      </div>

      <RunConfiguration instance={config.instance} system={config.system} />

      <div className="overflow-hidden rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
        <div className="mb-4 flex items-center justify-between">
          <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Performance Heatmap</h3>
          <input
            type="text"
            placeholder="Filter tests..."
            value={q}
            onChange={(e) => handleSearchChange(e.target.value)}
            className="rounded-xs border border-gray-300 bg-white px-3 py-1 text-sm/6 placeholder-gray-400 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 dark:placeholder-gray-500"
          />
        </div>
        <TestHeatmap
          tests={result.tests}
          suiteTests={suite?.tests}
          runId={runId}
          suiteHash={config.suite_hash}
          selectedTest={testModal}
          statusFilter={status}
          searchQuery={q}
          sortMode={heatmapSort}
          threshold={heatmapThreshold}
          stepFilter={stepFilter}
          onSelectedTestChange={handleTestModalChange}
          onSortModeChange={handleHeatmapSortChange}
          onThresholdChange={handleHeatmapThresholdChange}
          onSearchChange={handleSearchChange}
        />
      </div>

      {suite?.tests && suite.tests.length > 0 && (
        <div className="overflow-hidden rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
          <OpcodeHeatmap
            tests={suite.tests}
            extraColumns={[{
              name: 'Mgas/s',
              getValue: (testIndex: number) => {
                const testName = suite.tests[testIndex]?.name
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
            onTestClick={(testIndex) => handleTestModalChange(suite.tests[testIndex - 1]?.name)}
            searchQuery={q}
            onSearchChange={handleSearchChange}
          />
        </div>
      )}

      <ResourceUsageCharts
        tests={result.tests}
        onTestClick={handleTestModalChange}
        resourceCollectionMethod={config.system_resource_collection_method}
      />

      {result.pre_run_steps && Object.keys(result.pre_run_steps).length > 0 && (
        <PreRunStepsTable
          preRunSteps={result.pre_run_steps}
          suitePreRunSteps={suite?.pre_run_steps}
          runId={runId}
          suiteHash={config.suite_hash}
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
    </div>
  )
}
