import { useMemo, useState, useEffect } from 'react'
import { Link, useParams, useNavigate, useSearch } from '@tanstack/react-router'
import { Tab, TabGroup, TabList, TabPanel, TabPanels } from '@headlessui/react'
import clsx from 'clsx'
import { useSuite } from '@/api/hooks/useSuite'
import { useSuiteStats } from '@/api/hooks/useSuiteStats'
import { useIndex } from '@/api/hooks/useIndex'
import { DurationChart, type XAxisMode } from '@/components/suite-detail/DurationChart'
import { MGasChart } from '@/components/suite-detail/MGasChart'
import { ResourceCharts } from '@/components/suite-detail/ResourceCharts'
import { RunsHeatmap, type ColorNormalization } from '@/components/suite-detail/RunsHeatmap'
import { TestHeatmap } from '@/components/suite-detail/TestHeatmap'
import { SuiteSource } from '@/components/suite-detail/SuiteSource'
import { TestFilesList } from '@/components/suite-detail/TestFilesList'
import { RunsTable, type SortColumn, type SortDirection } from '@/components/runs/RunsTable'
import { RunFilters, type TestStatusFilter } from '@/components/runs/RunFilters'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { Badge } from '@/components/shared/Badge'
import { JDenticon } from '@/components/shared/JDenticon'
import { Pagination } from '@/components/shared/Pagination'

const PAGE_SIZE_OPTIONS = [50, 100, 200] as const
const DEFAULT_PAGE_SIZE = 100

export function SuiteDetailPage() {
  const { suiteHash } = useParams({ from: '/suites/$suiteHash' })
  const navigate = useNavigate()
  const search = useSearch({ from: '/suites/$suiteHash' }) as {
    tab?: string
    client?: string
    image?: string
    status?: TestStatusFilter
    sortBy?: SortColumn
    sortDir?: SortDirection
    expanded?: number
    filesPage?: number
    q?: string
    chartMode?: XAxisMode
    mgasChartMode?: XAxisMode
    resourceChartMode?: XAxisMode
    heatmapColor?: ColorNormalization
  }
  const { tab, client, image, status = 'all', sortBy = 'timestamp', sortDir = 'desc', expanded, filesPage, q, chartMode = 'runCount', mgasChartMode = 'runCount', resourceChartMode = 'runCount', heatmapColor = 'suite' } = search
  const { data: suite, isLoading, error, refetch } = useSuite(suiteHash)
  const { data: suiteStats } = useSuiteStats(suiteHash)
  const { data: index } = useIndex()
  const [runsPage, setRunsPage] = useState(1)
  const [runsPageSize, setRunsPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [heatmapExpanded, setHeatmapExpanded] = useState(true)
  const [slowestTestsExpanded, setSlowestTestsExpanded] = useState(true)
  const [chartExpanded, setChartExpanded] = useState(true)
  const [mgasChartExpanded, setMgasChartExpanded] = useState(true)
  const [resourceChartsExpanded, setResourceChartsExpanded] = useState(true)
  const [isDark, setIsDark] = useState(() => {
    if (typeof window === 'undefined') return false
    return document.documentElement.classList.contains('dark')
  })

  useEffect(() => {
    const observer = new MutationObserver(() => {
      setIsDark(document.documentElement.classList.contains('dark'))
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [])

  const suiteRunsAll = useMemo(() => {
    if (!index) return []
    return index.entries.filter((entry) => entry.suite_hash === suiteHash)
  }, [index, suiteHash])

  const clients = useMemo(() => {
    const clientSet = new Set(suiteRunsAll.map((e) => e.instance.client))
    return Array.from(clientSet).sort()
  }, [suiteRunsAll])

  const images = useMemo(() => {
    const imageSet = new Set(suiteRunsAll.map((e) => e.instance.image))
    return Array.from(imageSet).sort()
  }, [suiteRunsAll])

  const filteredRuns = useMemo(() => {
    return suiteRunsAll.filter((e) => {
      if (client && e.instance.client !== client) return false
      if (image && e.instance.image !== image) return false
      if (status === 'passing' && e.tests.fail > 0) return false
      if (status === 'failing' && e.tests.fail === 0) return false
      return true
    })
  }, [suiteRunsAll, client, image, status])

  const totalRunsPages = Math.ceil(filteredRuns.length / runsPageSize)
  const paginatedRuns = filteredRuns.slice((runsPage - 1) * runsPageSize, runsPage * runsPageSize)

  const handleRunsPageSizeChange = (newSize: number) => {
    setRunsPageSize(newSize)
    setRunsPage(1)
  }

  const handleClientChange = (newClient: string | undefined) => {
    setRunsPage(1)
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client: newClient, image, status, sortBy, sortDir },
    })
  }

  const handleImageChange = (newImage: string | undefined) => {
    setRunsPage(1)
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image: newImage, status, sortBy, sortDir },
    })
  }

  const handleStatusChange = (newStatus: TestStatusFilter) => {
    setRunsPage(1)
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status: newStatus, sortBy, sortDir },
    })
  }

  if (isLoading) {
    return <LoadingState message="Loading suite details..." />
  }

  if (error) {
    return <ErrorState message={error.message} retry={() => refetch()} />
  }

  if (!suite) {
    return <ErrorState message="Suite not found" />
  }

  const hasWarmup = suite.warmup && suite.warmup.length > 0
  const warmupTabIndex = hasWarmup ? 2 : -1

  const getTabIndex = () => {
    if (tab === 'tests') return 1
    if (tab === 'warmup' && hasWarmup) return warmupTabIndex
    return 0 // runs is default
  }

  const handleTabChange = (index: number) => {
    let newTab: string
    if (index === 0) {
      newTab = 'runs'
    } else if (index === 1) {
      newTab = 'tests'
    } else {
      newTab = 'warmup'
    }
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab: newTab, client, image, status, sortBy, sortDir, expanded: undefined, filesPage: undefined, q: undefined },
    })
  }

  const handleSortChange = (newSortBy: SortColumn, newSortDir: SortDirection) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy: newSortBy, sortDir: newSortDir },
    })
  }

  const handleExpandedChange = (index: number | undefined) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, expanded: index, filesPage, q },
    })
  }

  const handleFilesPageChange = (page: number) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, expanded, filesPage: page, q },
    })
  }

  const handleSearchChange = (query: string | undefined) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, expanded: undefined, filesPage: 1, q: query || undefined, chartMode },
    })
  }

  const handleChartModeChange = (mode: XAxisMode) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode: mode, mgasChartMode, resourceChartMode, heatmapColor },
    })
  }

  const handleMgasChartModeChange = (mode: XAxisMode) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, mgasChartMode: mode, resourceChartMode, heatmapColor },
    })
  }

  const handleResourceChartModeChange = (mode: XAxisMode) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, mgasChartMode, resourceChartMode: mode, heatmapColor },
    })
  }

  const handleHeatmapColorChange = (mode: ColorNormalization) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, mgasChartMode, resourceChartMode, heatmapColor: mode },
    })
  }

  const handleRunClick = (runId: string) => {
    navigate({
      to: '/runs/$runId',
      params: { runId },
    })
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center gap-2 text-sm/6 text-gray-500 dark:text-gray-400">
        <Link to="/suites" className="hover:text-gray-700 dark:hover:text-gray-300">
          Suites
        </Link>
        <span>/</span>
        <span className="font-mono text-gray-900 dark:text-gray-100">{suiteHash}</span>
      </div>

      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3">
          <JDenticon value={suite.hash} size={40} className="shrink-0 rounded-xs" />
          <h1 className="font-mono text-2xl/8 font-bold text-gray-900 dark:text-gray-100">{suite.hash}</h1>
        </div>
        {suite.filter && <Badge variant="info">Filter: {suite.filter}</Badge>}
      </div>

      <TabGroup selectedIndex={getTabIndex()} onChange={handleTabChange}>
        <TabList className="flex gap-1 rounded-sm bg-gray-100 p-1 dark:bg-gray-800">
          <Tab
            className={({ selected }) =>
              clsx(
                'flex cursor-pointer items-center gap-2 rounded-sm px-4 py-2 text-sm/6 font-medium transition-colors focus:outline-hidden',
                selected
                  ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-700 dark:text-gray-100'
                  : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
              )
            }
          >
            Runs
            <Badge variant="info">{suiteRunsAll.length}</Badge>
          </Tab>
          <Tab
            className={({ selected }) =>
              clsx(
                'flex cursor-pointer items-center gap-2 rounded-sm px-4 py-2 text-sm/6 font-medium transition-colors focus:outline-hidden',
                selected
                  ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-700 dark:text-gray-100'
                  : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
              )
            }
          >
            Tests
            <Badge variant="default">{suite.tests.length}</Badge>
          </Tab>
          {hasWarmup && (
            <Tab
              className={({ selected }) =>
                clsx(
                  'flex cursor-pointer items-center gap-2 rounded-sm px-4 py-2 text-sm/6 font-medium transition-colors focus:outline-hidden',
                  selected
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-700 dark:text-gray-100'
                    : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                )
              }
            >
              Warmup
              <Badge variant="default">{suite.warmup!.length}</Badge>
            </Tab>
          )}
        </TabList>
        <TabPanels className="mt-4">
          <TabPanel>
            {suiteRunsAll.length === 0 ? (
              <p className="py-8 text-center text-sm/6 text-gray-500 dark:text-gray-400">
                No runs found for this suite.
              </p>
            ) : (
              <div className="flex flex-col gap-4">
                <div className="overflow-hidden rounded-sm border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
                  <button
                    onClick={() => setHeatmapExpanded(!heatmapExpanded)}
                    className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm/6 font-medium text-gray-900 hover:bg-gray-50 dark:text-gray-100 dark:hover:bg-gray-700/50"
                  >
                    <svg
                      className={clsx('size-4 text-gray-500 transition-transform', heatmapExpanded && 'rotate-90')}
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                    >
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                    </svg>
                    Recent Runs by Client
                  </button>
                  {heatmapExpanded && (
                    <div className="border-t border-gray-200 p-4 dark:border-gray-700">
                      <RunsHeatmap runs={suiteRunsAll} isDark={isDark} colorNormalization={heatmapColor} onColorNormalizationChange={handleHeatmapColorChange} />
                    </div>
                  )}
                </div>
                <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
                  <div className="overflow-hidden rounded-sm border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
                    <button
                      onClick={() => setChartExpanded(!chartExpanded)}
                      className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm/6 font-medium text-gray-900 hover:bg-gray-50 dark:text-gray-100 dark:hover:bg-gray-700/50"
                    >
                      <svg
                        className={clsx('size-4 text-gray-500 transition-transform', chartExpanded && 'rotate-90')}
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                      >
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                      </svg>
                      Duration Chart
                    </button>
                    {chartExpanded && (
                      <div className="border-t border-gray-200 p-4 dark:border-gray-700">
                        <DurationChart
                          runs={suiteRunsAll}
                          isDark={isDark}
                          xAxisMode={chartMode}
                          onXAxisModeChange={handleChartModeChange}
                          onRunClick={handleRunClick}
                        />
                      </div>
                    )}
                  </div>
                  <div className="overflow-hidden rounded-sm border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
                    <button
                      onClick={() => setMgasChartExpanded(!mgasChartExpanded)}
                      className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm/6 font-medium text-gray-900 hover:bg-gray-50 dark:text-gray-100 dark:hover:bg-gray-700/50"
                    >
                      <svg
                        className={clsx('size-4 text-gray-500 transition-transform', mgasChartExpanded && 'rotate-90')}
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                      >
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                      </svg>
                      MGas/s Chart
                    </button>
                    {mgasChartExpanded && (
                      <div className="border-t border-gray-200 p-4 dark:border-gray-700">
                        <MGasChart
                          runs={suiteRunsAll}
                          isDark={isDark}
                          xAxisMode={mgasChartMode}
                          onXAxisModeChange={handleMgasChartModeChange}
                          onRunClick={handleRunClick}
                        />
                      </div>
                    )}
                  </div>
                </div>
                <div className="overflow-hidden rounded-sm border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
                  <button
                    onClick={() => setResourceChartsExpanded(!resourceChartsExpanded)}
                    className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm/6 font-medium text-gray-900 hover:bg-gray-50 dark:text-gray-100 dark:hover:bg-gray-700/50"
                  >
                    <svg
                      className={clsx('size-4 text-gray-500 transition-transform', resourceChartsExpanded && 'rotate-90')}
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                    >
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                    </svg>
                    Resource Usage
                  </button>
                  {resourceChartsExpanded && (
                    <div className="border-t border-gray-200 p-4 dark:border-gray-700">
                      <ResourceCharts
                        runs={suiteRunsAll}
                        isDark={isDark}
                        xAxisMode={resourceChartMode}
                        onXAxisModeChange={handleResourceChartModeChange}
                        onRunClick={handleRunClick}
                      />
                    </div>
                  )}
                </div>
                {suiteStats && Object.keys(suiteStats).length > 0 && (
                  <div className="overflow-hidden rounded-sm border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
                    <button
                      onClick={() => setSlowestTestsExpanded(!slowestTestsExpanded)}
                      className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm/6 font-medium text-gray-900 hover:bg-gray-50 dark:text-gray-100 dark:hover:bg-gray-700/50"
                    >
                      <svg
                        className={clsx('size-4 text-gray-500 transition-transform', slowestTestsExpanded && 'rotate-90')}
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                      >
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                      </svg>
                      Test Heatmap
                    </button>
                    {slowestTestsExpanded && (
                      <div className="border-t border-gray-200 p-4 dark:border-gray-700">
                        <TestHeatmap stats={suiteStats} testFiles={suite.tests} isDark={isDark} />
                      </div>
                    )}
                  </div>
                )}
                <RunFilters
                  clients={clients}
                  selectedClient={client}
                  onClientChange={handleClientChange}
                  images={images}
                  selectedImage={image}
                  onImageChange={handleImageChange}
                  selectedStatus={status}
                  onStatusChange={handleStatusChange}
                />
                {filteredRuns.length === 0 ? (
                  <p className="py-8 text-center text-sm/6 text-gray-500 dark:text-gray-400">
                    No runs match the selected filters.
                  </p>
                ) : (
                  <>
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <span className="text-sm/6 text-gray-500 dark:text-gray-400">Show</span>
                        <select
                          value={runsPageSize}
                          onChange={(e) => handleRunsPageSizeChange(Number(e.target.value))}
                          className="rounded-sm border border-gray-300 bg-white px-2 py-1 text-sm/6 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                        >
                          {PAGE_SIZE_OPTIONS.map((size) => (
                            <option key={size} value={size}>
                              {size}
                            </option>
                          ))}
                        </select>
                        <span className="text-sm/6 text-gray-500 dark:text-gray-400">per page</span>
                      </div>
                      {totalRunsPages > 1 && (
                        <Pagination currentPage={runsPage} totalPages={totalRunsPages} onPageChange={setRunsPage} />
                      )}
                    </div>
                    <RunsTable entries={paginatedRuns} sortBy={sortBy} sortDir={sortDir} onSortChange={handleSortChange} />
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-2">
                        <span className="text-sm/6 text-gray-500 dark:text-gray-400">Show</span>
                        <select
                          value={runsPageSize}
                          onChange={(e) => handleRunsPageSizeChange(Number(e.target.value))}
                          className="rounded-sm border border-gray-300 bg-white px-2 py-1 text-sm/6 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                        >
                          {PAGE_SIZE_OPTIONS.map((size) => (
                            <option key={size} value={size}>
                              {size}
                            </option>
                          ))}
                        </select>
                        <span className="text-sm/6 text-gray-500 dark:text-gray-400">per page</span>
                      </div>
                      {totalRunsPages > 1 && (
                        <Pagination currentPage={runsPage} totalPages={totalRunsPages} onPageChange={setRunsPage} />
                      )}
                    </div>
                  </>
                )}
              </div>
            )}
          </TabPanel>
          <TabPanel className="flex flex-col gap-4">
            {suite.source.tests && <SuiteSource title="Source" source={suite.source.tests} />}
            <TestFilesList
              files={suite.tests}
              suiteHash={suiteHash}
              type="tests"
              expandedIndex={expanded}
              onExpandedChange={handleExpandedChange}
              currentPage={filesPage}
              onPageChange={handleFilesPageChange}
              searchQuery={q}
              onSearchChange={handleSearchChange}
            />
          </TabPanel>
          {hasWarmup && (
            <TabPanel className="flex flex-col gap-4">
              {suite.source.warmup && <SuiteSource title="Source" source={suite.source.warmup} />}
              <TestFilesList
                files={suite.warmup!}
                suiteHash={suiteHash}
                type="warmup"
                expandedIndex={expanded}
                onExpandedChange={handleExpandedChange}
                currentPage={filesPage}
                onPageChange={handleFilesPageChange}
                searchQuery={q}
                onSearchChange={handleSearchChange}
              />
            </TabPanel>
          )}
        </TabPanels>
      </TabGroup>
    </div>
  )
}
