import { Link, useParams, useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { fetchText } from '@/api/client'
import { useRunConfig } from '@/api/hooks/useRunConfig'
import { useRunResult } from '@/api/hooks/useRunResult'
import { useSuite } from '@/api/hooks/useSuite'
import { RunConfiguration } from '@/components/run-detail/RunConfiguration'
import { ResourceUsageCharts } from '@/components/run-detail/ResourceUsageCharts'
import { TestsTable, type TestSortColumn, type TestSortDirection, type TestStatusFilter } from '@/components/run-detail/TestsTable'
import { TestHeatmap, type SortMode } from '@/components/run-detail/TestHeatmap'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { ClientStat } from '@/components/shared/ClientStat'
import { Duration } from '@/components/shared/Duration'
import { JDenticon } from '@/components/shared/JDenticon'
import { formatTimestamp } from '@/utils/date'
import { formatNumber, formatBytes } from '@/utils/format'

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
  }
  const page = Number(search.page) || 1
  const pageSize = Number(search.pageSize) || 20
  const heatmapThreshold = search.heatmapThreshold ? Number(search.heatmapThreshold) : undefined
  const { sortBy = 'order', sortDir = 'asc', q = '', status = 'all', testModal, heatmapSort } = search

  const { data: config, isLoading: configLoading, error: configError, refetch: refetchConfig } = useRunConfig(runId)
  const { data: result, isLoading: resultLoading, error: resultError, refetch: refetchResult } = useRunResult(runId)
  const { data: suite } = useSuite(config?.suite_hash ?? '')
  const { data: containerLog } = useQuery({
    queryKey: ['run', runId, 'container-log'],
    queryFn: () => fetchText(`runs/${runId}/container.log`),
    enabled: !!runId,
  })
  const { data: benchmarkoorLog } = useQuery({
    queryKey: ['run', runId, 'benchmarkoor-log'],
    queryFn: () => fetchText(`runs/${runId}/benchmarkoor.log`),
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

  const testCount = Object.keys(result.tests).length
  const passedTests = Object.values(result.tests).filter((t) => t.aggregated.fail === 0).length
  const failedTests = Object.values(result.tests).filter((t) => t.aggregated.fail > 0).length
  const totalDuration = Object.values(result.tests).reduce((sum, t) => sum + t.aggregated.time_total, 0)
  const totalGasUsed = Object.values(result.tests).reduce((sum, t) => sum + t.aggregated.gas_used_total, 0)
  const totalGasUsedTime = Object.values(result.tests).reduce((sum, t) => sum + t.aggregated.gas_used_time_total, 0)
  const mgasPerSec = totalGasUsedTime > 0 ? (totalGasUsed * 1000) / totalGasUsedTime : undefined
  const totalMsgCount = Object.values(result.tests).reduce((sum, t) => sum + t.aggregated.msg_count, 0)
  const methodCounts = Object.values(result.tests).reduce<Record<string, number>>((acc, t) => {
    Object.entries(t.aggregated.method_stats.times).forEach(([method, stats]) => {
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
          <p className="text-sm/6 font-medium text-gray-500 dark:text-gray-400">MGas/s</p>
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
        <h3 className="mb-4 text-sm/6 font-medium text-gray-900 dark:text-gray-100">Performance Heatmap</h3>
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
          onSelectedTestChange={handleTestModalChange}
          onSortModeChange={handleHeatmapSortChange}
          onThresholdChange={handleHeatmapThresholdChange}
        />
      </div>

      <ResourceUsageCharts
        tests={result.tests}
        onTestClick={handleTestModalChange}
        resourceCollectionMethod={config.system_resource_collection_method}
      />

      <TestsTable
        tests={result.tests}
        suiteTests={suite?.tests}
        currentPage={page}
        pageSize={pageSize}
        sortBy={sortBy}
        sortDir={sortDir}
        searchQuery={q}
        statusFilter={status}
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
