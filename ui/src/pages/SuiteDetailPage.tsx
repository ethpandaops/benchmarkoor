import { useCallback, useMemo, useState, useEffect } from 'react'
import { Link, useParams, useNavigate, useSearch } from '@tanstack/react-router'
import { Tab, TabGroup, TabList, TabPanel, TabPanels } from '@headlessui/react'
import clsx from 'clsx'
import { ChevronRight, SquareStack, LayoutGrid, Clock, Grid3X3 } from 'lucide-react'
import { type IndexStepType, ALL_INDEX_STEP_TYPES, DEFAULT_INDEX_STEP_FILTER, type SuiteTest } from '@/api/types'
import { useSuite } from '@/api/hooks/useSuite'
import { useSuiteStats } from '@/api/hooks/useSuiteStats'
import { useIndex } from '@/api/hooks/useIndex'
import { DurationChart, type XAxisMode } from '@/components/suite-detail/DurationChart'
import { MGasChart } from '@/components/suite-detail/MGasChart'
import { ResourceCharts } from '@/components/suite-detail/ResourceCharts'
import { RunsHeatmap, type ColorNormalization } from '@/components/suite-detail/RunsHeatmap'
import { TestHeatmap } from '@/components/suite-detail/TestHeatmap'
import { SuiteSource } from '@/components/suite-detail/SuiteSource'
import { TestFilesList, type OpcodeSortMode } from '@/components/suite-detail/TestFilesList'
import { OpcodeHeatmap } from '@/components/suite-detail/OpcodeHeatmap'
import { RunsTable } from '@/components/runs/RunsTable'
import { sortIndexEntries, type SortColumn, type SortDirection } from '@/components/runs/sortEntries'
import { RunFilters, type TestStatusFilter } from '@/components/runs/RunFilters'
import { LoadingState, Spinner } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { Badge } from '@/components/shared/Badge'
import { JDenticon } from '@/components/shared/JDenticon'
import { Pagination } from '@/components/shared/Pagination'

const PAGE_SIZE_OPTIONS = [50, 100, 200] as const
const DEFAULT_PAGE_SIZE = 100

// Parse step filter from URL (comma-separated string) or use default
function parseStepFilter(param: string | undefined): IndexStepType[] {
  if (!param) return DEFAULT_INDEX_STEP_FILTER
  const steps = param.split(',').filter((s): s is IndexStepType => ALL_INDEX_STEP_TYPES.includes(s as IndexStepType))
  return steps.length > 0 ? steps : DEFAULT_INDEX_STEP_FILTER
}

// Serialize step filter to URL param (undefined if default)
function serializeStepFilter(steps: IndexStepType[]): string | undefined {
  const sorted = [...steps].sort()
  const defaultSorted = [...DEFAULT_INDEX_STEP_FILTER].sort()
  if (sorted.length === defaultSorted.length && sorted.every((s, i) => s === defaultSorted[i])) {
    return undefined
  }
  return steps.join(',')
}

function OpcodeHeatmapSection({ tests, onTestClick }: { tests: SuiteTest[]; onTestClick?: (testIndex: number) => void }) {
  const [expanded, setExpanded] = useState(true)
  return (
    <>
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm/6 font-medium text-gray-900 hover:bg-gray-50 dark:text-gray-100 dark:hover:bg-gray-700/50"
      >
        <ChevronRight className={clsx('size-4 text-gray-500 transition-transform', expanded && 'rotate-90')} />
        <Grid3X3 className="size-4 text-gray-400 dark:text-gray-500" />
        Opcode Heatmap
      </button>
      {expanded && (
        <div className="border-t border-gray-200 p-4 dark:border-gray-700">
          <OpcodeHeatmap tests={tests} onTestClick={onTestClick} />
        </div>
      )}
    </>
  )
}

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
    filesPage?: number
    detail?: number
    opcodeSort?: OpcodeSortMode
    q?: string
    chartMode?: XAxisMode
    chartPassingOnly?: string
    heatmapColor?: ColorNormalization
    steps?: string
    hq?: string
    hn?: string
    hr?: string
    hFs?: string
    hStat?: string
    hCs?: string
    hTh?: string
    hRpc?: string
    hPs?: string
  }
  const { tab, client, image, status = 'all', sortBy = 'timestamp', sortDir = 'desc', filesPage, detail, opcodeSort, q, chartMode = 'runCount', heatmapColor = 'suite', hq, hn, hr, hFs, hStat, hCs, hTh, hRpc, hPs } = search
  const chartPassingOnly = search.chartPassingOnly !== 'false'
  const stepFilter = parseStepFilter(search.steps)
  const { data: suite, isLoading, error, refetch } = useSuite(suiteHash)
  const { data: suiteStats, isLoading: suiteStatsLoading } = useSuiteStats(suiteHash)
  const { data: index } = useIndex()
  const [runsPage, setRunsPage] = useState(1)
  const [runsPageSize, setRunsPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [compareMode, setCompareMode] = useState(false)
  const [selectedRunIds, setSelectedRunIds] = useState<Set<string>>(new Set())

  const handleSelectionChange = useCallback((runId: string, selected: boolean) => {
    setSelectedRunIds((prev) => {
      const next = new Set(prev)
      if (selected) {
        if (next.size >= 2) return prev
        next.add(runId)
      } else {
        next.delete(runId)
      }
      return next
    })
  }, [])

  const handleExitCompareMode = useCallback(() => {
    setCompareMode(false)
    setSelectedRunIds(new Set())
  }, [])

  const [heatmapExpanded, setHeatmapExpanded] = useState(true)
  const [chartExpanded, setChartExpanded] = useState(true)
  const [chartZoomRange, setChartZoomRange] = useState({ start: 0, end: 100 })
  const handleChartZoomChange = useCallback((range: { start: number; end: number }) => {
    setChartZoomRange(range)
  }, [])

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

  // Filter to only completed runs for metrics (exclude container_died, cancelled)
  // Runs without status are considered completed (backward compatibility)
  const completedRuns = useMemo(() => {
    return suiteRunsAll.filter((entry) => !entry.status || entry.status === 'completed')
  }, [suiteRunsAll])

  const chartRuns = useMemo(() => {
    if (!chartPassingOnly) return completedRuns
    return completedRuns.filter((entry) => entry.tests.tests_total === entry.tests.tests_passed)
  }, [completedRuns, chartPassingOnly])

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
      if (status === 'passing' && e.tests.tests_total - e.tests.tests_passed > 0) return false
      if (status === 'failing' && e.tests.tests_total - e.tests.tests_passed === 0) return false
      return true
    })
  }, [suiteRunsAll, client, image, status])

  const sortedRuns = useMemo(() => sortIndexEntries(filteredRuns, sortBy, sortDir, stepFilter), [filteredRuns, sortBy, sortDir, stepFilter])
  const totalRunsPages = Math.ceil(sortedRuns.length / runsPageSize)
  const paginatedRuns = sortedRuns.slice((runsPage - 1) * runsPageSize, runsPage * runsPageSize)

  const handleRunsPageSizeChange = (newSize: number) => {
    setRunsPageSize(newSize)
    setRunsPage(1)
  }

  const handleClientChange = (newClient: string | undefined) => {
    setRunsPage(1)
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client: newClient, image, status, sortBy, sortDir, steps: serializeStepFilter(stepFilter) },
    })
  }

  const handleImageChange = (newImage: string | undefined) => {
    setRunsPage(1)
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image: newImage, status, sortBy, sortDir, steps: serializeStepFilter(stepFilter) },
    })
  }

  const handleStatusChange = (newStatus: TestStatusFilter) => {
    setRunsPage(1)
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status: newStatus, sortBy, sortDir, steps: serializeStepFilter(stepFilter) },
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

  const hasPreRunSteps = suite.pre_run_steps && suite.pre_run_steps.length > 0

  // Tab order: runs(0), tests(1), pre_run_steps(2, conditional), source(last)
  const sourceTabIndex = hasPreRunSteps ? 3 : 2

  const getTabIndex = () => {
    if (tab === 'tests') return 1
    if (tab === 'pre_run_steps' && hasPreRunSteps) return 2
    if (tab === 'source') return sourceTabIndex
    return 0 // runs is default
  }

  const handleTabChange = (index: number) => {
    let newTab: string
    if (index === 0) {
      newTab = 'runs'
    } else if (index === 1) {
      newTab = 'tests'
    } else if (index === sourceTabIndex) {
      newTab = 'source'
    } else {
      newTab = 'pre_run_steps'
    }
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab: newTab, client, image, status, sortBy, sortDir, filesPage: undefined, detail: undefined, opcodeSort: undefined, q: undefined },
    })
  }

  const handleSortChange = (newSortBy: SortColumn, newSortDir: SortDirection) => {
    setRunsPage(1)
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy: newSortBy, sortDir: newSortDir, steps: serializeStepFilter(stepFilter) },
    })
  }

  const handleFilesPageChange = (page: number) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, filesPage: page, q, steps: serializeStepFilter(stepFilter) },
    })
  }

  const handleSearchChange = (query: string | undefined) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, filesPage: 1, q: query || undefined, chartMode, steps: serializeStepFilter(stepFilter) },
    })
  }

  const handleDetailChange = (index: number | undefined) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, filesPage, detail: index, opcodeSort, q, steps: serializeStepFilter(stepFilter) },
    })
  }

  const handleOpcodeSortChange = (sort: OpcodeSortMode) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, filesPage, detail, opcodeSort: sort === 'name' ? undefined : sort, q, steps: serializeStepFilter(stepFilter) },
    })
  }

  const chartPassingOnlyParam = chartPassingOnly ? undefined : 'false'

  const handleChartModeChange = (mode: XAxisMode) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode: mode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter) },
    })
  }

  const handleChartPassingOnlyChange = (passingOnly: boolean) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: passingOnly ? undefined : 'false', heatmapColor, steps: serializeStepFilter(stepFilter) },
    })
  }

  const handleHeatmapColorChange = (mode: ColorNormalization) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor: mode, steps: serializeStepFilter(stepFilter) },
    })
  }

  const handleHeatmapSearchChange = (query: string | undefined) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq: query || undefined, hn, hr, hFs, hStat, hCs, hTh, hRpc, hPs },
    })
  }

  const handleHeatmapShowNameChange = (show: boolean) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn: show ? '1' : undefined, hr, hFs, hStat, hCs, hTh, hRpc, hPs },
    })
  }

  const handleHeatmapRegexChange = (useRegex: boolean) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn, hr: useRegex ? '1' : undefined, hFs, hStat, hCs, hTh, hRpc, hPs },
    })
  }

  const handleHeatmapFullscreenChange = (fs: boolean) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn, hr, hFs: fs ? '1' : undefined, hStat, hCs, hTh, hRpc, hPs },
    })
  }

  const handleHeatmapStatChange = (stat: string) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn, hr, hFs, hStat: stat === 'avgMgas' ? undefined : stat, hCs, hTh, hRpc, hPs },
    })
  }

  const handleHeatmapClientStatChange = (show: boolean) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn, hr, hFs, hStat, hCs: show ? '1' : undefined, hTh, hRpc, hPs },
    })
  }

  const handleHeatmapThresholdChange = (th: number) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn, hr, hFs, hStat, hCs, hTh: th === 60 ? undefined : String(th), hRpc, hPs },
    })
  }

  const handleHeatmapRunsPerClientChange = (count: number) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn, hr, hFs, hStat, hCs, hTh, hRpc: count === 5 ? undefined : String(count), hPs },
    })
  }

  const handleHeatmapPageSizeChange = (size: number) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn, hr, hFs, hStat, hCs, hTh, hRpc, hPs: size === 20 ? undefined : String(size) },
    })
  }

  const handleStepFilterChange = (steps: IndexStepType[]) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(steps) },
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
        <span className="font-mono text-gray-900 dark:text-gray-100">
          {suite.metadata?.labels?.name ?? suiteHash}
        </span>
      </div>

      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex items-center gap-3">
          <JDenticon value={suite.hash} size={40} className="shrink-0 rounded-xs" />
          <div className="flex flex-col">
            {suite.metadata?.labels?.name ? (
              <>
                <h1 className="text-2xl/8 font-bold text-gray-900 dark:text-gray-100">
                  {suite.metadata.labels.name}
                </h1>
                <span className="font-mono text-sm/6 text-gray-500 dark:text-gray-400">
                  {suite.hash}
                </span>
              </>
            ) : (
              <h1 className="font-mono text-2xl/8 font-bold text-gray-900 dark:text-gray-100">
                {suite.hash}
              </h1>
            )}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {suite.filter && <Badge variant="info">Filter: {suite.filter}</Badge>}
          {suite.metadata?.labels &&
            Object.entries(suite.metadata.labels)
              .filter(([key]) => key !== 'name')
              .map(([key, value]) => (
                <Badge key={key} variant="default">
                  {key}: {value}
                </Badge>
              ))}
        </div>
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
          {hasPreRunSteps && (
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
              Pre-Run Steps
              <Badge variant="default">{suite.pre_run_steps!.length}</Badge>
            </Tab>
          )}
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
            Source
          </Tab>
        </TabList>
        <TabPanels className="mt-4">
          <TabPanel>
            {suiteRunsAll.length === 0 ? (
              <p className="py-8 text-center text-sm/6 text-gray-500 dark:text-gray-400">
                No runs found for this suite.
              </p>
            ) : (
              <div className="flex flex-col gap-4">
                {/* Step Filter Control */}
                <div className="flex items-center gap-3 rounded-sm bg-white p-3 shadow-xs dark:bg-gray-800">
                  <span className="text-sm/6 font-medium text-gray-700 dark:text-gray-300">Metric steps:</span>
                  <div className="flex items-center gap-1">
                    {ALL_INDEX_STEP_TYPES.map((step) => (
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
                        className={`rounded-sm px-2.5 py-1 text-xs font-medium capitalize transition-colors ${
                          stepFilter.includes(step)
                            ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300'
                            : 'bg-gray-100 text-gray-400 dark:bg-gray-700 dark:text-gray-500'
                        }`}
                        title={`${stepFilter.includes(step) ? 'Exclude' : 'Include'} ${step} step in metric calculations`}
                      >
                        {step}
                      </button>
                    ))}
                  </div>
                  <span className="text-xs text-gray-500 dark:text-gray-400">
                    (affects Duration, MGas/s calculations)
                  </span>
                </div>
                <div className="overflow-hidden rounded-sm border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
                  <button
                    onClick={() => setHeatmapExpanded(!heatmapExpanded)}
                    className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm/6 font-medium text-gray-900 hover:bg-gray-50 dark:text-gray-100 dark:hover:bg-gray-700/50"
                  >
                    <ChevronRight className={clsx('size-4 text-gray-500 transition-transform', heatmapExpanded && 'rotate-90')} />
                    <LayoutGrid className="size-4 text-gray-400 dark:text-gray-500" />
                    Recent Runs by Client
                  </button>
                  {heatmapExpanded && (
                    <div className="border-t border-gray-200 p-4 dark:border-gray-700">
                      <RunsHeatmap runs={suiteRunsAll} isDark={isDark} colorNormalization={heatmapColor} onColorNormalizationChange={handleHeatmapColorChange} stepFilter={stepFilter} />
                    </div>
                  )}
                </div>
                <div className="overflow-hidden rounded-sm border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
                  <button
                    onClick={() => setChartExpanded(!chartExpanded)}
                    className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm/6 font-medium text-gray-900 hover:bg-gray-50 dark:text-gray-100 dark:hover:bg-gray-700/50"
                  >
                    <ChevronRight className={clsx('size-4 text-gray-500 transition-transform', chartExpanded && 'rotate-90')} />
                    <Clock className="size-4 text-gray-400 dark:text-gray-500" />
                    Run Charts
                  </button>
                  {chartExpanded && (
                    <div className="flex flex-col gap-4 border-t border-gray-200 p-4 dark:border-gray-700">
                      <div className="flex items-center justify-end gap-4">
                        <label className="flex cursor-pointer items-center gap-2">
                          <span className="text-xs font-medium text-gray-600 dark:text-gray-400">Passing runs only</span>
                          <button
                            role="switch"
                            aria-checked={chartPassingOnly}
                            onClick={() => handleChartPassingOnlyChange(!chartPassingOnly)}
                            className={clsx(
                              'relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors',
                              chartPassingOnly ? 'bg-green-500' : 'bg-gray-300 dark:bg-gray-600',
                            )}
                          >
                            <span
                              className={clsx(
                                'inline-block size-3.5 rounded-full bg-white transition-transform',
                                chartPassingOnly ? 'translate-x-4.5' : 'translate-x-0.5',
                              )}
                            />
                          </button>
                        </label>
                        <div className="inline-flex rounded-sm border border-gray-300 dark:border-gray-600">
                          <button
                            onClick={() => handleChartModeChange('runCount')}
                            className={clsx(
                              'px-3 py-1 text-xs/5 font-medium transition-colors',
                              chartMode === 'runCount'
                                ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900'
                                : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700',
                            )}
                          >
                            Run #
                          </button>
                          <button
                            onClick={() => handleChartModeChange('time')}
                            className={clsx(
                              'border-l border-gray-300 px-3 py-1 text-xs/5 font-medium transition-colors dark:border-gray-600',
                              chartMode === 'time'
                                ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900'
                                : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700',
                            )}
                          >
                            Time
                          </button>
                        </div>
                      </div>
                      <div className="flex items-center gap-3">
                        <div className="h-px grow bg-gray-200 dark:bg-gray-700" />
                        <span className="text-xs font-medium text-gray-400 dark:text-gray-500">Performance</span>
                        <div className="h-px grow bg-gray-200 dark:bg-gray-700" />
                      </div>
                      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
                        <div className="rounded-sm bg-gray-50 p-3 dark:bg-gray-700/50">
                          <DurationChart
                            runs={chartRuns}
                            isDark={isDark}
                            xAxisMode={chartMode}
                            onXAxisModeChange={handleChartModeChange}
                            onRunClick={handleRunClick}
                            stepFilter={stepFilter}
                            hideControls
                            zoomRange={chartZoomRange}
                            onZoomChange={handleChartZoomChange}
                          />
                        </div>
                        <div className="rounded-sm bg-gray-50 p-3 dark:bg-gray-700/50">
                          <MGasChart
                            runs={chartRuns}
                            isDark={isDark}
                            xAxisMode={chartMode}
                            onXAxisModeChange={handleChartModeChange}
                            onRunClick={handleRunClick}
                            stepFilter={stepFilter}
                            hideControls
                            zoomRange={chartZoomRange}
                            onZoomChange={handleChartZoomChange}
                          />
                        </div>
                      </div>
                      <div className="flex items-center gap-3">
                        <div className="h-px grow bg-gray-200 dark:bg-gray-700" />
                        <span className="text-xs font-medium text-gray-400 dark:text-gray-500">System Resources</span>
                        <div className="h-px grow bg-gray-200 dark:bg-gray-700" />
                      </div>
                      <ResourceCharts
                        runs={chartRuns}
                        isDark={isDark}
                        xAxisMode={chartMode}
                        onXAxisModeChange={handleChartModeChange}
                        onRunClick={handleRunClick}
                        hideControls
                        zoomRange={chartZoomRange}
                        onZoomChange={handleChartZoomChange}
                      />
                      {chartMode === 'runCount' && (
                        <div className="flex justify-end text-xs/5 text-gray-500 dark:text-gray-400">
                          <span>&larr; Older runs | More recent &rarr;</span>
                        </div>
                      )}
                    </div>
                  )}
                </div>
                <div className="flex flex-wrap items-end gap-4">
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
                </div>
                {filteredRuns.length === 0 ? (
                  <p className="py-8 text-center text-sm/6 text-gray-500 dark:text-gray-400">
                    No runs match the selected filters.
                  </p>
                ) : (
                  <>
                    <div className="flex items-center justify-between">
                      <div className="flex items-center gap-3">
                        <button
                          onClick={() => compareMode ? handleExitCompareMode() : setCompareMode(true)}
                          className={`flex cursor-pointer items-center justify-center rounded-sm p-1.5 shadow-xs ring-1 ring-inset transition-colors ${
                            compareMode
                              ? 'bg-blue-600 text-white ring-blue-600 hover:bg-blue-700 hover:ring-blue-700'
                              : 'bg-white text-gray-500 ring-gray-300 hover:bg-gray-50 hover:text-gray-700 dark:bg-gray-800 dark:text-gray-400 dark:ring-gray-600 dark:hover:bg-gray-700 dark:hover:text-gray-200'
                          }`}
                          title="Compare"
                        >
                          <SquareStack className="size-4" />
                        </button>
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
                    <RunsTable entries={paginatedRuns} sortBy={sortBy} sortDir={sortDir} onSortChange={handleSortChange} stepFilter={stepFilter} selectable={compareMode} selectedRunIds={selectedRunIds} onSelectionChange={handleSelectionChange} />
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
            {(suiteStatsLoading || (suiteStats && Object.keys(suiteStats).length > 0)) && (
              suiteStatsLoading ? (
                <div className="overflow-hidden rounded-sm border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
                  <div className="flex items-center justify-center gap-2 py-8">
                    <Spinner size="md" />
                    <span className="text-sm/6 text-gray-500 dark:text-gray-400">Loading test heatmap...</span>
                  </div>
                </div>
              ) : (
                <TestHeatmap stats={suiteStats!} testFiles={suite.tests} isDark={isDark} isLoading={suiteStatsLoading} suiteHash={suiteHash} suiteName={suite.metadata?.labels?.name} stepFilter={stepFilter} searchQuery={hq} onSearchChange={handleHeatmapSearchChange} showTestName={hn === '1'} onShowTestNameChange={handleHeatmapShowNameChange} showClientStat={hCs === '1'} onShowClientStatChange={handleHeatmapClientStatChange} useRegex={hr === '1'} onUseRegexChange={handleHeatmapRegexChange} fullscreen={hFs === '1'} onFullscreenChange={handleHeatmapFullscreenChange} histogramStat={(hStat as 'avgMgas' | 'minMgas' | 'p99Mgas') || undefined} onHistogramStatChange={handleHeatmapStatChange} threshold={hTh ? Number(hTh) : undefined} onThresholdChange={handleHeatmapThresholdChange} runsPerClient={hRpc ? Number(hRpc) : undefined} onRunsPerClientChange={handleHeatmapRunsPerClientChange} pageSize={hPs ? Number(hPs) : undefined} onPageSizeChange={handleHeatmapPageSizeChange} />
              )
            )}
            {suite.tests.some((t) => t.eest?.info?.opcode_count && Object.keys(t.eest.info.opcode_count).length > 0) && (
              <div className="overflow-hidden rounded-sm border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
                <OpcodeHeatmapSection tests={suite.tests} onTestClick={handleDetailChange} />
              </div>
            )}
            <TestFilesList
              tests={suite.tests}
              suiteHash={suiteHash}
              type="tests"
              currentPage={filesPage}
              onPageChange={handleFilesPageChange}
              searchQuery={q}
              onSearchChange={handleSearchChange}
              detailIndex={detail}
              onDetailChange={handleDetailChange}
              opcodeSort={opcodeSort}
              onOpcodeSortChange={handleOpcodeSortChange}
            />
          </TabPanel>
          {hasPreRunSteps && (
            <TabPanel className="flex flex-col gap-4">
              <TestFilesList
                files={suite.pre_run_steps!}
                suiteHash={suiteHash}
                type="pre_run_steps"
                currentPage={filesPage}
                onPageChange={handleFilesPageChange}
                searchQuery={q}
                onSearchChange={handleSearchChange}
                detailIndex={detail}
                onDetailChange={handleDetailChange}
              />
            </TabPanel>
          )}
          <TabPanel>
            <SuiteSource title="Source" source={suite.source} />
          </TabPanel>
        </TabPanels>
      </TabGroup>

      {compareMode && (
        <div className="fixed inset-x-0 bottom-0 z-50 border-t border-gray-200 bg-white px-6 py-3 shadow-sm dark:border-gray-700 dark:bg-gray-800">
          <div className="mx-auto flex max-w-7xl items-center justify-between">
            <span className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">
              {selectedRunIds.size} of 2 selected
            </span>
            <div className="flex items-center gap-2">
              <button
                onClick={handleExitCompareMode}
                className="rounded-sm px-3 py-1.5 text-sm/6 font-medium text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-700"
              >
                Cancel
              </button>
              <button
                disabled={selectedRunIds.size !== 2}
                onClick={() => {
                  const ids = Array.from(selectedRunIds)
                  navigate({ to: '/compare', search: { a: ids[0], b: ids[1] } })
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
