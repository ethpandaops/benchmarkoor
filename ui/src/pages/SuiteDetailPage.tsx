import { useCallback, useMemo, useState, useEffect, useRef } from 'react'
import { Link, useParams, useNavigate, useSearch } from '@tanstack/react-router'
import { Tab, TabGroup, TabList, TabPanel, TabPanels } from '@headlessui/react'
import clsx from 'clsx'
import { ChevronRight, SquareStack, GitCompareArrows, LayoutGrid, Clock, Grid3X3, Trash2, Plus, X } from 'lucide-react'
import { type IndexEntry, type IndexStepType, ALL_INDEX_STEP_TYPES, DEFAULT_INDEX_STEP_FILTER, type SuiteTest } from '@/api/types'
import { useSuite } from '@/api/hooks/useSuite'
import { useSuiteStats } from '@/api/hooks/useSuiteStats'
import { useIndex } from '@/api/hooks/useIndex'
import { useDeleteRuns } from '@/api/hooks/useAdmin'
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
import { parseLabelFilters, serializeLabelFilters, type LabelFilters } from '@/components/runs/labelFilterUtils'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { getBaseClient } from '@/utils/client-colors'
import { LoadingState, Spinner } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { Badge } from '@/components/shared/Badge'
import { JDenticon } from '@/components/shared/JDenticon'
import { Pagination } from '@/components/shared/Pagination'
import { MAX_COMPARE_RUNS, MIN_COMPARE_RUNS } from '@/components/compare/constants'
import { useAuth } from '@/hooks/useAuth'

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

// ---------------------------------------------------------------------------
// ChartFilters — client toggles + label chip filters for the Run Charts
// ---------------------------------------------------------------------------

interface ChartFiltersProps {
  clients: string[]
  clientFilter: Set<string>
  onClientFilterChange: (filter: Set<string>) => void
  labelKeys: string[]
  labelValues: Map<string, string[]>
  labelFilters: Map<string, Set<string>>
  onLabelFiltersChange: (filters: Map<string, Set<string>>) => void
}

function ChartFilters({
  clients, clientFilter, onClientFilterChange,
  labelKeys, labelValues, labelFilters, onLabelFiltersChange,
}: ChartFiltersProps) {
  const [keyDropdownOpen, setKeyDropdownOpen] = useState(false)
  const [valueDropdownKey, setValueDropdownKey] = useState<string | null>(null)
  const keyRef = useRef<HTMLDivElement>(null)
  const valueRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (keyDropdownOpen && keyRef.current && !keyRef.current.contains(e.target as Node)) setKeyDropdownOpen(false)
      if (valueDropdownKey && valueRef.current && !valueRef.current.contains(e.target as Node)) setValueDropdownKey(null)
    }
    document.addEventListener('mousedown', handleClick)
    return () => document.removeEventListener('mousedown', handleClick)
  }, [keyDropdownOpen, valueDropdownKey])

  const toggleClient = (c: string) => {
    const next = new Set(clientFilter)
    if (next.has(c)) { next.delete(c) } else { next.add(c) }
    onClientFilterChange(next)
  }

  const availableLabelKeys = labelKeys.filter((k) => !labelFilters.has(k) && k !== valueDropdownKey)

  const toggleLabelValue = (key: string, value: string) => {
    const next = new Map(labelFilters)
    const values = new Set(next.get(key) ?? [])
    if (values.has(value)) {
      values.delete(value)
      if (values.size === 0) next.delete(key)
      else next.set(key, values)
    } else {
      values.add(value)
      next.set(key, values)
    }
    onLabelFiltersChange(next)
  }

  const removeLabelFilter = (key: string) => {
    const next = new Map(labelFilters)
    next.delete(key)
    onLabelFiltersChange(next)
    if (valueDropdownKey === key) setValueDropdownKey(null)
  }

  const chipKeys = Array.from(labelFilters.keys())
  if (valueDropdownKey && !labelFilters.has(valueDropdownKey)) chipKeys.push(valueDropdownKey)

  return (
    <div className="flex flex-wrap items-center gap-2">
      {/* Client toggles */}
      {clients.map((c) => {
        const active = clientFilter.size === 0 || clientFilter.has(c)
        return (
          <button
            key={c}
            onClick={() => toggleClient(c)}
            className={clsx(
              'transition-opacity',
              !active && 'opacity-30 hover:opacity-60',
            )}
          >
            <ClientBadge client={c} />
          </button>
        )
      })}

      {/* Label filter chips */}
      {chipKeys.map((key) => {
        const values = labelFilters.get(key)
        const isPending = !values
        return (
          <div key={key} className="relative" ref={valueDropdownKey === key ? valueRef : undefined}>
            <div
              role="button"
              tabIndex={0}
              onClick={() => setValueDropdownKey(valueDropdownKey === key ? null : key)}
              onKeyDown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); setValueDropdownKey(valueDropdownKey === key ? null : key) } }}
              className={clsx(
                'flex cursor-pointer items-center gap-1.5 rounded-xs border px-2 py-1 text-xs/5 font-medium transition-colors',
                isPending
                  ? 'border-dashed border-blue-300 bg-blue-50/50 text-blue-500 dark:border-blue-700 dark:bg-blue-900/20 dark:text-blue-400'
                  : 'border-blue-200 bg-blue-50 text-blue-700 hover:bg-blue-100 dark:border-blue-800 dark:bg-blue-900/30 dark:text-blue-300 dark:hover:bg-blue-900/50',
              )}
            >
              <span className="font-semibold">{key}</span>
              {values && values.size > 0 && (
                <>
                  <span>=</span>
                  <span>{Array.from(values).join(', ')}</span>
                </>
              )}
              <button
                onClick={(e) => { e.stopPropagation(); if (isPending) setValueDropdownKey(null); else removeLabelFilter(key) }}
                className="ml-0.5 rounded-xs p-0.5 hover:bg-blue-200 dark:hover:bg-blue-800"
              >
                <X className="size-3" />
              </button>
            </div>
            {valueDropdownKey === key && (
              <div className="absolute top-full left-0 z-50 mt-1 max-h-64 min-w-48 overflow-y-auto rounded-xs border border-gray-200 bg-white shadow-sm dark:border-gray-700 dark:bg-gray-800">
                {(labelValues.get(key) ?? []).map((val) => {
                  const selected = values?.has(val) ?? false
                  return (
                    <button
                      key={val}
                      onClick={() => toggleLabelValue(key, val)}
                      className={clsx(
                        'flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm/6 transition-colors',
                        selected
                          ? 'bg-blue-50 text-blue-700 dark:bg-blue-900/30 dark:text-blue-300'
                          : 'text-gray-700 hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-700',
                      )}
                    >
                      <span className={clsx(
                        'flex size-4 shrink-0 items-center justify-center rounded-xs border text-xs/3',
                        selected
                          ? 'border-blue-500 bg-blue-500 text-white dark:border-blue-400 dark:bg-blue-400'
                          : 'border-gray-300 dark:border-gray-600',
                      )}>
                        {selected && '✓'}
                      </span>
                      {val}
                    </button>
                  )
                })}
              </div>
            )}
          </div>
        )
      })}

      {/* Add label filter button */}
      {availableLabelKeys.length > 0 && (
        <div className="relative" ref={keyRef}>
          <button
            onClick={() => { setKeyDropdownOpen(!keyDropdownOpen); setValueDropdownKey(null) }}
            className="flex items-center gap-1 rounded-xs border border-dashed border-gray-300 px-2 py-1 text-xs/5 text-gray-500 transition-colors hover:border-gray-400 hover:text-gray-700 dark:border-gray-600 dark:text-gray-400 dark:hover:border-gray-500 dark:hover:text-gray-300"
          >
            <Plus className="size-3" />
            Label
          </button>
          {keyDropdownOpen && (
            <div className="absolute top-full left-0 z-50 mt-1 min-w-36 overflow-hidden rounded-xs border border-gray-200 bg-white shadow-sm dark:border-gray-700 dark:bg-gray-800">
              {availableLabelKeys.map((key) => (
                <button
                  key={key}
                  onClick={() => { setKeyDropdownOpen(false); setValueDropdownKey(key) }}
                  className="flex w-full px-3 py-1.5 text-left text-sm/6 text-gray-700 transition-colors hover:bg-gray-50 dark:text-gray-300 dark:hover:bg-gray-700"
                >
                  {key}
                </button>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

export function SuiteDetailPage() {
  const { suiteHash } = useParams({ from: '/suites/$suiteHash' })
  const navigate = useNavigate()
  const { isAdmin } = useAuth()
  const deleteRuns = useDeleteRuns()
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
    labels?: string
    hq?: string
    hn?: string
    hr?: string
    hFs?: string
    hStat?: string
    hCs?: string
    hTh?: string
    hRpc?: string
    hPs?: string
    groupBy?: string
  }
  const { tab, client, image, status = 'all', sortBy = 'timestamp', sortDir = 'desc', filesPage, detail, opcodeSort, q, chartMode = 'runCount', heatmapColor = 'suite', hq, hn, hr, hFs, hStat, hCs, hTh, hRpc, hPs, groupBy } = search
  const chartPassingOnly = search.chartPassingOnly !== 'false'
  const stepFilter = parseStepFilter(search.steps)
  const labelFilters = parseLabelFilters(search.labels)
  const { data: suite, isLoading, error, refetch } = useSuite(suiteHash)
  const { data: suiteStats, isLoading: suiteStatsLoading } = useSuiteStats(suiteHash)
  const { data: index } = useIndex()
  const [runsPage, setRunsPage] = useState(1)
  const [runsPageSize, setRunsPageSize] = useState(DEFAULT_PAGE_SIZE)
  const [compareMode, setCompareMode] = useState(false)
  const [selectedRunIds, setSelectedRunIds] = useState<Set<string>>(new Set())

  // Delete mode state (mutually exclusive with compare mode)
  const [deleteMode, setDeleteMode] = useState(false)
  const [deleteSelectedIds, setDeleteSelectedIds] = useState<Set<string>>(new Set())

  const handleSelectionChange = useCallback((runId: string, selected: boolean) => {
    setSelectedRunIds((prev) => {
      const next = new Set(prev)
      if (selected) {
        if (next.size >= MAX_COMPARE_RUNS) return prev
        next.add(runId)
      } else {
        next.delete(runId)
      }
      return next
    })
  }, [])

  const handleDeleteSelectionChange = useCallback((runId: string, selected: boolean) => {
    setDeleteSelectedIds((prev) => {
      const next = new Set(prev)
      if (selected) {
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

  const handleEnterDeleteMode = useCallback(() => {
    setCompareMode(false)
    setSelectedRunIds(new Set())
    setDeleteMode(true)
  }, [])

  const handleExitDeleteMode = useCallback(() => {
    setDeleteMode(false)
    setDeleteSelectedIds(new Set())
  }, [])

  const handleEnterCompareMode = useCallback(() => {
    setDeleteMode(false)
    setDeleteSelectedIds(new Set())
    setCompareMode(true)
  }, [])

  const handleDeleteConfirm = useCallback(() => {
    if (deleteSelectedIds.size === 0) return
    if (!window.confirm(`Delete ${deleteSelectedIds.size} run(s)? This cannot be undone.`)) return
    deleteRuns.mutate(Array.from(deleteSelectedIds), {
      onSuccess: () => {
        handleExitDeleteMode()
      },
    })
  }, [deleteSelectedIds, deleteRuns, handleExitDeleteMode])

  const [heatmapExpanded, setHeatmapExpanded] = useState(true)
  const [chartExpanded, setChartExpanded] = useState(true)
  const [chartZoomRange, setChartZoomRange] = useState({ start: 0, end: 100 })
  const [chartClientFilter, setChartClientFilter] = useState<Set<string>>(new Set())
  const [chartLabelFilters, setChartLabelFilters] = useState<Map<string, Set<string>>>(new Map())
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

  // Collect available label keys for the group-by selector
  const groupByLabelKeys = useMemo(() => {
    const keys = new Set<string>()
    for (const run of suiteRunsAll) {
      if (run.metadata) {
        for (const key of Object.keys(run.metadata)) {
          if (!key.startsWith('github.') && key !== 'name') keys.add(key)
        }
      }
    }
    return Array.from(keys).sort()
  }, [suiteRunsAll])

  // Apply grouping: transforms client name to include the group suffix
  const applyGrouping = useCallback((runs: IndexEntry[]): IndexEntry[] => {
    if (!groupBy) return runs
    return runs.map((run) => {
      let suffix: string
      if (groupBy === 'instance_id') {
        suffix = run.instance.id
      } else {
        suffix = run.metadata?.[groupBy] ?? '(none)'
      }
      return {
        ...run,
        instance: { ...run.instance, client: `${run.instance.client} / ${suffix}` },
      }
    })
  }, [groupBy])

  const groupedRunsAll = useMemo(() => applyGrouping(suiteRunsAll), [suiteRunsAll, applyGrouping])

  // Filter to only completed runs for metrics (exclude container_died, cancelled)
  // Runs without status are considered completed (backward compatibility)
  const completedRuns = useMemo(() => {
    return groupedRunsAll.filter((entry) => !entry.status || entry.status === 'completed')
  }, [groupedRunsAll])

  const chartRunsBase = useMemo(() => {
    if (!chartPassingOnly) return completedRuns
    return completedRuns.filter((entry) => entry.tests.tests_total === entry.tests.tests_passed)
  }, [completedRuns, chartPassingOnly])

  // Derive available base clients and label keys/values for chart filtering
  const { chartClients, chartLabelKeys, chartLabelValues } = useMemo(() => {
    const clientSet = new Set<string>()
    const valMap = new Map<string, Set<string>>()
    for (const run of chartRunsBase) {
      clientSet.add(getBaseClient(run.instance.client))
      if (run.metadata) {
        for (const [key, value] of Object.entries(run.metadata)) {
          if (key.startsWith('github.') || key === 'name') continue
          let set = valMap.get(key)
          if (!set) { set = new Set(); valMap.set(key, set) }
          set.add(value)
        }
      }
    }
    const keys = Array.from(valMap.keys()).sort()
    const values = new Map<string, string[]>()
    for (const [key, set] of valMap) values.set(key, Array.from(set).sort())
    return { chartClients: Array.from(clientSet).sort(), chartLabelKeys: keys, chartLabelValues: values }
  }, [chartRunsBase])

  const chartRuns = useMemo(() => {
    let runs = chartRunsBase
    if (chartClientFilter.size > 0) {
      runs = runs.filter((r) => chartClientFilter.has(getBaseClient(r.instance.client)))
    }
    if (chartLabelFilters.size > 0) {
      runs = runs.filter((r) => {
        for (const [key, allowed] of chartLabelFilters) {
          const actual = r.metadata?.[key]
          if (!actual || !allowed.has(actual)) return false
        }
        return true
      })
    }
    return runs
  }, [chartRunsBase, chartClientFilter, chartLabelFilters])

  const clients = useMemo(() => {
    const clientSet = new Set(suiteRunsAll.map((e) => e.instance.client))
    return Array.from(clientSet).sort()
  }, [suiteRunsAll])

  // Most recent successful run per client, for quick cross-client comparison.
  const recentSuccessfulPerClient = useMemo(() => {
    const sorted = [...completedRuns].sort((a, b) => b.timestamp - a.timestamp)
    const seen = new Set<string>()
    const result: typeof completedRuns = []

    for (const run of sorted) {
      if (seen.has(run.instance.client)) continue
      if (run.tests.tests_total > 0 && run.tests.tests_passed === run.tests.tests_total) {
        seen.add(run.instance.client)
        result.push(run)
      }
      if (result.length >= MAX_COMPARE_RUNS) break
    }

    return result
  }, [completedRuns])

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
      if (status === 'timeout' && e.status !== 'timeout') return false
      if (status === 'cancelled' && e.status !== 'cancelled') return false
      for (const [key, allowedValues] of labelFilters) {
        const actual = e.metadata?.[key]
        if (!actual || !allowedValues.has(actual)) return false
      }
      return true
    })
  }, [suiteRunsAll, client, image, status, labelFilters])

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
      search: { tab, client: newClient, image, status, sortBy, sortDir, steps: serializeStepFilter(stepFilter), labels: serializeLabelFilters(labelFilters), groupBy },
    })
  }

  const handleImageChange = (newImage: string | undefined) => {
    setRunsPage(1)
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image: newImage, status, sortBy, sortDir, steps: serializeStepFilter(stepFilter), labels: serializeLabelFilters(labelFilters), groupBy },
    })
  }

  const handleStatusChange = (newStatus: TestStatusFilter) => {
    setRunsPage(1)
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status: newStatus, sortBy, sortDir, steps: serializeStepFilter(stepFilter), labels: serializeLabelFilters(labelFilters), groupBy },
    })
  }

  const handleLabelFiltersChange = (newFilters: LabelFilters) => {
    setRunsPage(1)
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, steps: serializeStepFilter(stepFilter), labels: serializeLabelFilters(newFilters), groupBy },
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
      search: { tab: newTab, client, image, status, sortBy, sortDir, filesPage: undefined, detail: undefined, opcodeSort: undefined, q: undefined, groupBy },
    })
  }

  const handleSortChange = (newSortBy: SortColumn, newSortDir: SortDirection) => {
    setRunsPage(1)
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy: newSortBy, sortDir: newSortDir, steps: serializeStepFilter(stepFilter), labels: serializeLabelFilters(labelFilters), groupBy },
    })
  }

  const handleFilesPageChange = (page: number) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, filesPage: page, q, steps: serializeStepFilter(stepFilter), labels: serializeLabelFilters(labelFilters), groupBy },
    })
  }

  const handleSearchChange = (query: string | undefined) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, filesPage: 1, q: query || undefined, chartMode, steps: serializeStepFilter(stepFilter), labels: serializeLabelFilters(labelFilters), groupBy },
    })
  }

  const handleDetailChange = (index: number | undefined) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, filesPage, detail: index, opcodeSort, q, steps: serializeStepFilter(stepFilter), labels: serializeLabelFilters(labelFilters), groupBy },
    })
  }

  const handleOpcodeSortChange = (sort: OpcodeSortMode) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, filesPage, detail, opcodeSort: sort === 'name' ? undefined : sort, q, steps: serializeStepFilter(stepFilter), labels: serializeLabelFilters(labelFilters), groupBy },
    })
  }

  const chartPassingOnlyParam = chartPassingOnly ? undefined : 'false'

  const handleChartModeChange = (mode: XAxisMode) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode: mode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), labels: serializeLabelFilters(labelFilters), groupBy },
    })
  }

  const handleChartPassingOnlyChange = (passingOnly: boolean) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: passingOnly ? undefined : 'false', heatmapColor, steps: serializeStepFilter(stepFilter), labels: serializeLabelFilters(labelFilters), groupBy },
    })
  }

  const handleHeatmapColorChange = (mode: ColorNormalization) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor: mode, steps: serializeStepFilter(stepFilter), labels: serializeLabelFilters(labelFilters), groupBy },
    })
  }

  const handleHeatmapSearchChange = (query: string | undefined) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq: query || undefined, hn, hr, hFs, hStat, hCs, hTh, hRpc, hPs, groupBy },
    })
  }

  const handleHeatmapShowNameChange = (show: boolean) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn: show ? '1' : undefined, hr, hFs, hStat, hCs, hTh, hRpc, hPs, groupBy },
    })
  }

  const handleHeatmapRegexChange = (useRegex: boolean) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn, hr: useRegex ? '1' : undefined, hFs, hStat, hCs, hTh, hRpc, hPs, groupBy },
    })
  }

  const handleHeatmapFullscreenChange = (fs: boolean) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn, hr, hFs: fs ? '1' : undefined, hStat, hCs, hTh, hRpc, hPs, groupBy },
    })
  }

  const handleHeatmapStatChange = (stat: string) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn, hr, hFs, hStat: stat === 'avgMgas' ? undefined : stat, hCs, hTh, hRpc, hPs, groupBy },
    })
  }

  const handleHeatmapClientStatChange = (show: boolean) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn, hr, hFs, hStat, hCs: show ? '1' : undefined, hTh, hRpc, hPs, groupBy },
    })
  }

  const handleHeatmapThresholdChange = (th: number) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn, hr, hFs, hStat, hCs, hTh: th === 60 ? undefined : String(th), hRpc, hPs, groupBy },
    })
  }

  const handleHeatmapRunsPerClientChange = (count: number) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn, hr, hFs, hStat, hCs, hTh, hRpc: count === 5 ? undefined : String(count), hPs, groupBy },
    })
  }

  const handleHeatmapPageSizeChange = (size: number) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), hq, hn, hr, hFs, hStat, hCs, hTh, hRpc, hPs: size === 20 ? undefined : String(size), groupBy },
    })
  }

  const handleGroupByChange = (key: string | undefined) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(stepFilter), labels: serializeLabelFilters(labelFilters), groupBy: key },
    })
  }

  const handleStepFilterChange = (steps: IndexStepType[]) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, client, image, status, sortBy, sortDir, chartMode, chartPassingOnly: chartPassingOnlyParam, heatmapColor, steps: serializeStepFilter(steps), labels: serializeLabelFilters(labelFilters), groupBy },
    })
  }

  const handleRunClick = (runId: string) => {
    navigate({
      to: '/runs/$runId',
      params: { runId },
    })
  }

  const isSelectable = compareMode || deleteMode

  return (
    <div className="flex flex-col gap-6">
      <div className="flex min-w-0 items-center gap-2 text-sm/6 text-gray-500 dark:text-gray-400">
        <Link to="/suites" className="shrink-0 hover:text-gray-700 dark:hover:text-gray-300">
          Suites
        </Link>
        <span>/</span>
        <span className="truncate font-mono text-gray-900 dark:text-gray-100">
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
        <TabList className="flex gap-1 overflow-x-auto rounded-xs bg-gray-100 p-1 dark:bg-gray-800">
          <Tab
            className={({ selected }) =>
              clsx(
                'flex cursor-pointer items-center gap-1.5 whitespace-nowrap rounded-xs px-2.5 py-1.5 text-xs/5 font-medium transition-colors focus:outline-hidden sm:gap-2 sm:px-4 sm:py-2 sm:text-sm/6',
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
                'flex cursor-pointer items-center gap-1.5 whitespace-nowrap rounded-xs px-2.5 py-1.5 text-xs/5 font-medium transition-colors focus:outline-hidden sm:gap-2 sm:px-4 sm:py-2 sm:text-sm/6',
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
                  'flex cursor-pointer items-center gap-1.5 whitespace-nowrap rounded-xs px-2.5 py-1.5 text-xs/5 font-medium transition-colors focus:outline-hidden sm:gap-2 sm:px-4 sm:py-2 sm:text-sm/6',
                  selected
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-700 dark:text-gray-100'
                    : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                )
              }
            >
              <span className="sm:hidden">Pre-Run</span>
              <span className="hidden sm:inline">Pre-Run Steps</span>
              <Badge variant="default">{suite.pre_run_steps!.length}</Badge>
            </Tab>
          )}
          <Tab
            className={({ selected }) =>
              clsx(
                'flex cursor-pointer items-center gap-1.5 whitespace-nowrap rounded-xs px-2.5 py-1.5 text-xs/5 font-medium transition-colors focus:outline-hidden sm:gap-2 sm:px-4 sm:py-2 sm:text-sm/6',
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
                {/* Step Filter & Group By Controls */}
                <div className="flex flex-wrap items-center gap-x-6 gap-y-2 rounded-xs bg-white p-2 shadow-xs sm:p-3 dark:bg-gray-800">
                  <div className="flex flex-wrap items-center gap-2 sm:gap-3">
                    <span className="text-xs/5 font-medium text-gray-700 sm:text-sm/6 dark:text-gray-300">Metric steps:</span>
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
                          className={`rounded-xs px-2 py-0.5 text-xs font-medium capitalize transition-colors sm:px-2.5 sm:py-1 ${
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
                  </div>
                  {groupByLabelKeys.length > 0 && (
                    <div className="flex flex-wrap items-center gap-2 sm:gap-3">
                      <span className="text-xs/5 font-medium text-gray-700 sm:text-sm/6 dark:text-gray-300">Group by:</span>
                      <div className="flex items-center gap-1">
                        <button
                          onClick={() => handleGroupByChange(undefined)}
                          className={`rounded-xs px-2 py-0.5 text-xs font-medium transition-colors sm:px-2.5 sm:py-1 ${
                            !groupBy
                              ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                              : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600'
                          }`}
                        >
                          None
                        </button>
                        {groupByLabelKeys.map((key) => (
                          <button
                            key={key}
                            onClick={() => handleGroupByChange(key)}
                            className={`rounded-xs px-2 py-0.5 text-xs font-medium transition-colors sm:px-2.5 sm:py-1 ${
                              groupBy === key
                                ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                                : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600'
                            }`}
                          >
                            {key}
                          </button>
                        ))}
                        <button
                          onClick={() => handleGroupByChange('instance_id')}
                          className={`rounded-xs px-2 py-0.5 text-xs font-medium transition-colors sm:px-2.5 sm:py-1 ${
                            groupBy === 'instance_id'
                              ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                              : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600'
                          }`}
                        >
                          instance id
                        </button>
                      </div>
                    </div>
                  )}
                </div>
                <div className="overflow-hidden rounded-xs border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
                  <div className="flex flex-wrap items-center justify-between gap-2 px-3 py-2 sm:px-4 sm:py-3">
                    <button
                      onClick={() => setHeatmapExpanded(!heatmapExpanded)}
                      className="flex items-center gap-2 text-left text-sm/6 font-medium text-gray-900 hover:text-gray-700 dark:text-gray-100 dark:hover:text-gray-300"
                    >
                      <ChevronRight className={clsx('size-4 text-gray-500 transition-transform', heatmapExpanded && 'rotate-90')} />
                      <LayoutGrid className="size-4 text-gray-400 dark:text-gray-500" />
                      Recent Runs by Client
                    </button>
                    <div className="flex items-center gap-1.5">
                      <button
                        onClick={() => compareMode ? handleExitCompareMode() : handleEnterCompareMode()}
                        className={clsx(
                          'flex cursor-pointer items-center justify-center rounded-xs p-1 shadow-xs ring-1 ring-inset transition-colors',
                          compareMode
                            ? 'bg-blue-600 text-white ring-blue-600 hover:bg-blue-700 hover:ring-blue-700'
                            : 'bg-white text-gray-500 ring-gray-300 hover:bg-gray-50 hover:text-gray-700 dark:bg-gray-800 dark:text-gray-400 dark:ring-gray-600 dark:hover:bg-gray-700 dark:hover:text-gray-200',
                        )}
                        title="Compare"
                      >
                        <SquareStack className="size-3.5" />
                      </button>
                      <button
                        disabled={recentSuccessfulPerClient.length < MIN_COMPARE_RUNS}
                        onClick={() => {
                          const ids = recentSuccessfulPerClient.map((r) => r.run_id)
                          navigate({ to: '/compare', search: { runs: ids.join(',') } })
                        }}
                        className="flex cursor-pointer items-center justify-center rounded-xs p-1 shadow-xs ring-1 ring-inset transition-colors bg-white text-gray-500 ring-gray-300 hover:bg-gray-50 hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-gray-800 dark:text-gray-400 dark:ring-gray-600 dark:hover:bg-gray-700 dark:hover:text-gray-200"
                        title="Compare latest successful run per client"
                      >
                        <GitCompareArrows className="size-3.5" />
                      </button>
                    </div>
                  </div>
                  {heatmapExpanded && (
                    <div className="border-t border-gray-200 p-3 sm:p-4 dark:border-gray-700">
                      <RunsHeatmap
                        runs={suiteRunsAll}
                        groupBy={groupBy}
                        onCompareGroup={groupBy ? (groupRuns) => {
                          const sorted = [...groupRuns].sort((a, b) => b.timestamp - a.timestamp)
                          const seen = new Set<string>()
                          const ids: string[] = []
                          for (const run of sorted) {
                            if (seen.has(run.instance.client)) continue
                            if (run.tests.tests_total > 0 && run.tests.tests_passed === run.tests.tests_total) {
                              seen.add(run.instance.client)
                              ids.push(run.run_id)
                            }
                            if (ids.length >= MAX_COMPARE_RUNS) break
                          }
                          if (ids.length >= MIN_COMPARE_RUNS) {
                            navigate({ to: '/compare', search: { runs: ids.join(',') } })
                          }
                        } : undefined}
                        onCompareClientAcrossGroups={groupBy ? (client) => {
                          // Find the latest successful run for this client in each group
                          const sorted = [...suiteRunsAll]
                            .filter((r) => r.instance.client === client)
                            .sort((a, b) => b.timestamp - a.timestamp)
                          const seenGroups = new Set<string>()
                          const ids: string[] = []
                          for (const run of sorted) {
                            const groupValue = groupBy === 'instance_id'
                              ? run.instance.id
                              : (run.metadata?.[groupBy] ?? '(none)')
                            if (seenGroups.has(groupValue)) continue
                            if (run.tests.tests_total > 0 && run.tests.tests_passed === run.tests.tests_total) {
                              seenGroups.add(groupValue)
                              ids.push(run.run_id)
                            }
                            if (ids.length >= MAX_COMPARE_RUNS) break
                          }
                          if (ids.length >= MIN_COMPARE_RUNS) {
                            const labels = groupBy === 'instance_id' ? 'instance-id' : `label:${groupBy}`
                            navigate({ to: '/compare', search: { runs: ids.join(','), labels } })
                          }
                        } : undefined}
                        isDark={isDark}
                        colorNormalization={heatmapColor}
                        onColorNormalizationChange={handleHeatmapColorChange}
                        stepFilter={stepFilter}
                        selectable={compareMode}
                        selectedRunIds={selectedRunIds}
                        onSelectionChange={handleSelectionChange}
                      />
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
                      {/* Chart filters: client toggles + label chips */}
                      {(chartClients.length > 1 || chartLabelKeys.length > 0) && (
                        <ChartFilters
                          clients={chartClients}
                          clientFilter={chartClientFilter}
                          onClientFilterChange={setChartClientFilter}
                          labelKeys={chartLabelKeys}
                          labelValues={chartLabelValues}
                          labelFilters={chartLabelFilters}
                          onLabelFiltersChange={setChartLabelFilters}
                        />
                      )}
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
                    entries={suiteRunsAll}
                    labelFilters={labelFilters}
                    onLabelFiltersChange={handleLabelFiltersChange}
                  />
                </div>
                {filteredRuns.length === 0 ? (
                  <p className="py-8 text-center text-sm/6 text-gray-500 dark:text-gray-400">
                    No runs match the selected filters.
                  </p>
                ) : (
                  <>
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div className="flex items-center gap-3">
                        <button
                          onClick={() => compareMode ? handleExitCompareMode() : handleEnterCompareMode()}
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
                          disabled={recentSuccessfulPerClient.length < MIN_COMPARE_RUNS}
                          onClick={() => {
                            const ids = recentSuccessfulPerClient.map((r) => r.run_id)
                            navigate({ to: '/compare', search: { runs: ids.join(',') } })
                          }}
                          className="flex cursor-pointer items-center justify-center rounded-xs p-1.5 shadow-xs ring-1 ring-inset transition-colors bg-white text-gray-500 ring-gray-300 hover:bg-gray-50 hover:text-gray-700 disabled:cursor-not-allowed disabled:opacity-50 dark:bg-gray-800 dark:text-gray-400 dark:ring-gray-600 dark:hover:bg-gray-700 dark:hover:text-gray-200"
                          title="Compare latest successful run per client"
                        >
                          <GitCompareArrows className="size-4" />
                        </button>
                        {isAdmin && (
                          <button
                            onClick={() => deleteMode ? handleExitDeleteMode() : handleEnterDeleteMode()}
                            className={`flex cursor-pointer items-center justify-center rounded-xs p-1.5 shadow-xs ring-1 ring-inset transition-colors ${
                              deleteMode
                                ? 'bg-red-600 text-white ring-red-600 hover:bg-red-700 hover:ring-red-700'
                                : 'bg-white text-gray-500 ring-gray-300 hover:bg-gray-50 hover:text-gray-700 dark:bg-gray-800 dark:text-gray-400 dark:ring-gray-600 dark:hover:bg-gray-700 dark:hover:text-gray-200'
                            }`}
                            title="Delete runs"
                          >
                            <Trash2 className="size-4" />
                          </button>
                        )}
                        <span className="text-sm/6 text-gray-500 dark:text-gray-400">Show</span>
                        <select
                          value={runsPageSize}
                          onChange={(e) => handleRunsPageSizeChange(Number(e.target.value))}
                          className="rounded-xs border border-gray-300 bg-white px-2 py-1 text-sm/6 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                        >
                          {PAGE_SIZE_OPTIONS.map((size) => (
                            <option key={size} value={size}>
                              {size}
                            </option>
                          ))}
                        </select>
                        <span className="hidden text-sm/6 text-gray-500 sm:inline dark:text-gray-400">per page</span>
                      </div>
                      {totalRunsPages > 1 && (
                        <Pagination currentPage={runsPage} totalPages={totalRunsPages} onPageChange={setRunsPage} />
                      )}
                    </div>
                    <RunsTable
                      entries={paginatedRuns}
                      sortBy={sortBy}
                      sortDir={sortDir}
                      onSortChange={handleSortChange}
                      stepFilter={stepFilter}
                      selectable={isSelectable}
                      selectedRunIds={deleteMode ? deleteSelectedIds : selectedRunIds}
                      onSelectionChange={deleteMode ? handleDeleteSelectionChange : handleSelectionChange}
                      selectionVariant={deleteMode ? 'delete' : 'compare'}
                    />
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <div className="flex items-center gap-2">
                        <span className="text-sm/6 text-gray-500 dark:text-gray-400">Show</span>
                        <select
                          value={runsPageSize}
                          onChange={(e) => handleRunsPageSizeChange(Number(e.target.value))}
                          className="rounded-xs border border-gray-300 bg-white px-2 py-1 text-sm/6 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
                        >
                          {PAGE_SIZE_OPTIONS.map((size) => (
                            <option key={size} value={size}>
                              {size}
                            </option>
                          ))}
                        </select>
                        <span className="hidden text-sm/6 text-gray-500 sm:inline dark:text-gray-400">per page</span>
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

      {deleteMode && (
        <div className="fixed inset-x-0 bottom-0 z-50 border-t border-red-200 bg-white px-6 py-3 shadow-sm dark:border-red-800 dark:bg-gray-800">
          <div className="mx-auto flex max-w-7xl items-center justify-between">
            <span className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">
              {deleteSelectedIds.size} selected for deletion
            </span>
            <div className="flex items-center gap-2">
              <button
                onClick={handleExitDeleteMode}
                className="rounded-sm px-3 py-1.5 text-sm/6 font-medium text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-700"
              >
                Cancel
              </button>
              <button
                disabled={deleteSelectedIds.size === 0 || deleteRuns.isPending}
                onClick={handleDeleteConfirm}
                className="rounded-sm bg-red-600 px-4 py-1.5 text-sm/6 font-medium text-white hover:bg-red-700 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {deleteRuns.isPending ? 'Deleting...' : 'Delete'}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
