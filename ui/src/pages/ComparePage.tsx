import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import clsx from 'clsx'
import { Link, useSearch, useNavigate } from '@tanstack/react-router'
import { useQueries } from '@tanstack/react-query'
import { type IndexStepType, ALL_INDEX_STEP_TYPES } from '@/api/types'
import type { BlockLogs, RunConfig, RunResult } from '@/api/types'
import { fetchData } from '@/api/client'
import { useSuite } from '@/api/hooks/useSuite'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { JDenticon } from '@/components/shared/JDenticon'
import { CompareHeader } from '@/components/compare/CompareHeader'
import { StickyRunBar } from '@/components/compare/StickyRunBar'
import { MetricsComparison } from '@/components/compare/MetricsComparison'
import { MGasComparisonChart } from '@/components/compare/MGasComparisonChart'
import { type ChartType, CHART_TYPE_OPTIONS } from '@/components/compare/constants'
import { PercentageDiffChart } from '@/components/compare/PercentageDiffChart'
import { TestComparisonTable } from '@/components/compare/TestComparisonTable'
import { ResourceComparisonCharts } from '@/components/compare/ResourceComparisonCharts'
import { BlockLogsComparison } from '@/components/compare/BlockLogsComparison'
import { ConfigDiff } from '@/components/compare/ConfigDiff'
import { type StepTypeOption, ALL_STEP_TYPES, DEFAULT_STEP_FILTER } from '@/pages/RunDetailPage'
import { MIN_COMPARE_RUNS, MAX_COMPARE_RUNS, LABEL_MODE_OPTIONS, type CompareRun, type LabelMode } from '@/components/compare/constants'

function parseStepFilter(param: string | undefined): StepTypeOption[] {
  if (!param) return DEFAULT_STEP_FILTER
  const steps = param.split(',').filter((s): s is StepTypeOption => ALL_STEP_TYPES.includes(s as StepTypeOption))
  return steps.length > 0 ? steps : DEFAULT_STEP_FILTER
}

export function ComparePage() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/compare' }) as {
    runs?: string
    a?: string
    b?: string
    steps?: string
    baseline?: string
    labels?: string
    tableBase?: string
    sort?: string
    sortDir?: string
    diffFilter?: string
    filter?: string
    filterRegex?: string
  }

  // Backward-compat redirect: ?a=X&b=Y → ?runs=X,Y
  useEffect(() => {
    if (search.a && search.b && !search.runs) {
      navigate({
        to: '/compare',
        search: { runs: `${search.a},${search.b}`, steps: search.steps },
        replace: true,
      })
    }
  }, [search.a, search.b, search.runs, search.steps, navigate])

  const runIds = (search.runs ?? '')
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean)
    .slice(0, MAX_COMPARE_RUNS)

  const stepFilter = parseStepFilter(search.steps)
  const indexStepFilter: IndexStepType[] = stepFilter.filter(
    (s): s is IndexStepType => ALL_INDEX_STEP_TYPES.includes(s as IndexStepType),
  )
  void indexStepFilter

  const configQueries = useQueries({
    queries: runIds.map((runId) => ({
      queryKey: ['run', runId, 'config'],
      queryFn: async () => {
        const { data, status } = await fetchData<RunConfig>(`runs/${runId}/config.json`)
        if (!data) throw new Error(`Failed to fetch run config: ${status}`)
        return data
      },
      enabled: !!runId,
    })),
  })

  const resultQueries = useQueries({
    queries: runIds.map((runId) => ({
      queryKey: ['run', runId, 'result'],
      queryFn: async () => {
        const { data } = await fetchData<RunResult>(`runs/${runId}/result.json`)
        return data ?? null
      },
      enabled: !!runId,
    })),
  })

  const blockLogQueries = useQueries({
    queries: runIds.map((runId) => ({
      queryKey: ['run', runId, 'block-logs'],
      queryFn: async () => {
        const { data, status } = await fetchData<BlockLogs>(`runs/${runId}/result.block-logs.json`)
        if (!data) {
          if (status === 404) return null
          throw new Error(`Failed to fetch block logs: ${status}`)
        }
        return data
      },
      enabled: !!runId,
    })),
  })
  const blockLogsPerRun = blockLogQueries.map((q) => q.data ?? null)
  const blockLogsLoading = blockLogQueries.some((q) => q.isLoading)

  const suiteHash = configQueries.find((q) => q.data?.suite_hash)?.data?.suite_hash
  const { data: suite } = useSuite(suiteHash)
  const headerRef = useRef<HTMLDivElement>(null)
  const baselineIdx = Math.min(Math.max(parseInt(search.baseline ?? '0', 10) || 0, 0), runIds.length - 1)
  const labelMode: LabelMode = search.labels === 'instance-id' ? 'instance-id' : 'none'
  const tableBaseline: 'best' | 'worst' | number = search.tableBase === 'worst'
    ? 'worst'
    : search.tableBase !== undefined && search.tableBase !== 'best'
      ? Math.min(parseInt(search.tableBase, 10) || 0, runIds.length - 1)
      : 'best'

  const tableSortBy = (search.sort ?? 'order') as 'order' | 'name' | 'avgValue' | `run-${number}`
  const tableSortDir = (search.sortDir === 'desc' ? 'desc' : 'asc') as 'asc' | 'desc'
  const diffFilter = (search.diffFilter === 'faster' || search.diffFilter === 'slower' ? search.diffFilter : 'all') as 'all' | 'faster' | 'slower'
  const testFilter = search.filter ?? ''
  const testFilterRegex = search.filterRegex === '1'
  const [sharedZoom, setSharedZoom] = useState(true)
  const [chartZoom, setChartZoom] = useState({ start: 0, end: 100 })
  const [chartType, setChartType] = useState<ChartType>('line')

  const testNameFilter = useMemo(() => {
    if (!testFilter) return undefined
    if (testFilterRegex) {
      try {
        const re = new RegExp(testFilter, 'i')
        return (name: string) => re.test(name)
      } catch {
        return undefined
      }
    }
    const q = testFilter.toLowerCase()
    return (name: string) => name.toLowerCase().includes(q)
  }, [testFilter, testFilterRegex])

  const updateSearch = useCallback((patch: Record<string, string | undefined>) => {
    navigate({
      to: '/compare',
      search: { runs: search.runs, steps: search.steps, baseline: search.baseline, labels: search.labels, tableBase: search.tableBase, sort: search.sort, sortDir: search.sortDir, diffFilter: search.diffFilter, filter: search.filter, filterRegex: search.filterRegex, ...patch },
      replace: true,
    })
  }, [navigate, search.runs, search.steps, search.baseline, search.labels, search.tableBase, search.sort, search.sortDir, search.diffFilter, search.filter, search.filterRegex])

  const setBaselineIdx = useCallback((idx: number) => {
    updateSearch({ baseline: idx > 0 ? String(idx) : undefined })
  }, [updateSearch])
  const setLabelMode = useCallback((mode: LabelMode) => {
    updateSearch({ labels: mode === 'none' ? undefined : mode })
  }, [updateSearch])
  const setTableBaseline = useCallback((val: 'best' | 'worst' | number) => {
    updateSearch({ tableBase: val === 'best' ? undefined : String(val) })
  }, [updateSearch])
  const setTableSort = useCallback((column: string, direction: string) => {
    updateSearch({ sort: column === 'order' ? undefined : column, sortDir: direction === 'asc' ? undefined : direction })
  }, [updateSearch])
  const setDiffFilter = useCallback((val: 'all' | 'faster' | 'slower') => {
    updateSearch({ diffFilter: val === 'all' ? undefined : val })
  }, [updateSearch])
  const setTestFilter = useCallback((query: string) => {
    updateSearch({ filter: query || undefined })
  }, [updateSearch])
  const setTestFilterRegex = useCallback((enabled: boolean) => {
    updateSearch({ filterRegex: enabled ? '1' : undefined })
  }, [updateSearch])

  // Handle backward-compat redirect in progress
  if (search.a && search.b && !search.runs) {
    return <LoadingState message="Redirecting..." />
  }

  if (runIds.length < MIN_COMPARE_RUNS) {
    return <ErrorState message={`At least ${MIN_COMPARE_RUNS} run IDs are required. Use /compare?runs=id1,id2`} />
  }

  const isLoading = configQueries.some((q) => q.isLoading) || resultQueries.some((q) => q.isLoading)
  const error = configQueries.find((q) => q.error)?.error

  if (isLoading) {
    return <LoadingState message="Loading runs for comparison..." />
  }

  if (error) {
    return <ErrorState message={error.message} />
  }

  // Ensure all configs loaded
  const missingIdx = configQueries.findIndex((q) => !q.data)
  if (missingIdx !== -1) {
    return <ErrorState message={`Run not found: ${runIds[missingIdx]}`} />
  }

  const runs: CompareRun[] = runIds.map((runId, i) => ({
    runId,
    config: configQueries[i].data!,
    result: resultQueries[i].data ?? null,
    index: i,
  }))

  // Suite mismatch: check if all hashes are the same
  const uniqueHashes = new Set(runs.map((r) => r.config.suite_hash).filter(Boolean))
  const suiteMismatch = uniqueHashes.size > 1

  const allResults = runs.every((r) => r.result !== null)

  return (
    <div className="flex flex-col gap-6">
      <StickyRunBar runs={runs} sentinelRef={headerRef} labelMode={labelMode} onLabelModeChange={setLabelMode} testFilter={testFilter} testFilterRegex={testFilterRegex} onTestFilterChange={setTestFilter} onTestFilterRegexChange={setTestFilterRegex} />

      {/* Breadcrumb */}
      <div className="flex min-w-0 items-center gap-2 text-sm/6 text-gray-500 dark:text-gray-400">
        <Link to="/runs" className="shrink-0 hover:text-gray-700 dark:hover:text-gray-300">
          Runs
        </Link>
        <span>/</span>
        {suiteHash && (
          <>
            <Link
              to="/suites/$suiteHash"
              params={{ suiteHash }}
              className={`flex min-w-0 items-center gap-1.5 hover:text-gray-700 dark:hover:text-gray-300${suite?.metadata?.labels?.name ? '' : ' font-mono'}`}
            >
              <JDenticon value={suiteHash} size={16} className="shrink-0 rounded-xs" />
              <span className="truncate">{suite?.metadata?.labels?.name ?? suiteHash}</span>
            </Link>
            <span>/</span>
          </>
        )}
        <span className="shrink-0 text-gray-900 dark:text-gray-100">Compare</span>
      </div>

      <div className="flex flex-wrap items-center gap-4 text-xs/5 text-gray-500 dark:text-gray-400">
        <div className="flex items-center gap-1.5">
          <span>Labels:</span>
          <div className="flex gap-1">
            {LABEL_MODE_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                onClick={() => setLabelMode(opt.value)}
                className={`rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors ${
                  labelMode === opt.value
                    ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                    : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600'
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>
        <div className="flex items-center gap-1.5">
          <span>Chart:</span>
          <div className="flex gap-1">
            {CHART_TYPE_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                onClick={() => setChartType(opt.value)}
                className={`rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors ${
                  chartType === opt.value
                    ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                    : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600'
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>
        <div className="flex items-center gap-1.5">
          <span>Shared Zoom:</span>
          <button
            onClick={() => setSharedZoom(!sharedZoom)}
            className={`rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors ${
              sharedZoom
                ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600'
            }`}
          >
            {sharedZoom ? 'On' : 'Off'}
          </button>
        </div>
        <div className="flex items-center gap-1.5">
          <span>Filter:</span>
          <input
            type="text"
            placeholder={testFilterRegex ? 'Regex pattern...' : 'Filter tests...'}
            value={testFilter}
            onChange={(e) => setTestFilter(e.target.value)}
            className={clsx(
              'rounded-xs border bg-white px-3 py-1 text-sm/6 placeholder-gray-400 focus:outline-hidden focus:ring-1 dark:bg-gray-700 dark:text-gray-100 dark:placeholder-gray-500',
              testFilterRegex && testFilter && (() => { try { new RegExp(testFilter); return false } catch { return true } })()
                ? 'border-red-400 focus:border-red-500 focus:ring-red-500 dark:border-red-500'
                : 'border-gray-300 focus:border-blue-500 focus:ring-blue-500 dark:border-gray-600',
            )}
          />
          <button
            onClick={() => setTestFilterRegex(!testFilterRegex)}
            title={testFilterRegex ? 'Regex mode (click to switch to text)' : 'Text mode (click to switch to regex)'}
            className={clsx(
              'rounded-xs px-1.5 py-1 font-mono text-sm/6 transition-colors',
              testFilterRegex
                ? 'bg-blue-500 text-white'
                : 'border border-gray-300 bg-white text-gray-500 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
            )}
          >
            .*
          </button>
        </div>
      </div>

      {suiteMismatch && (
        <div className="rounded-sm border border-yellow-300 bg-yellow-50 p-3 text-sm/6 text-yellow-800 dark:border-yellow-700 dark:bg-yellow-900/20 dark:text-yellow-300">
          Warning: These runs belong to different suites. Test-level comparison may not be meaningful.
        </div>
      )}

      <div ref={headerRef}>
        <CompareHeader runs={runs} labelMode={labelMode} onRemoveRun={(id) => {
          const remaining = runIds.filter((r) => r !== id)
          navigate({ to: '/compare', search: { runs: remaining.join(','), steps: search.steps } })
        }} onMoveRun={(from, to) => {
          const reordered = [...runIds]
          const [moved] = reordered.splice(from, 1)
          reordered.splice(to, 0, moved)
          updateSearch({ runs: reordered.join(',') })
        }} />
      </div>

      <MetricsComparison runs={runs} stepFilter={stepFilter} baselineIdx={baselineIdx} onBaselineChange={setBaselineIdx} labelMode={labelMode} />

      {allResults && (
        <MGasComparisonChart runs={runs} suiteTests={suite?.tests} stepFilter={stepFilter} labelMode={labelMode} testNameFilter={testNameFilter} zoomRange={sharedZoom ? chartZoom : undefined} onZoomChange={sharedZoom ? setChartZoom : undefined} chartType={chartType} />
      )}

      {allResults && (
        <PercentageDiffChart runs={runs} suiteTests={suite?.tests} stepFilter={stepFilter} baselineIdx={baselineIdx} onBaselineChange={setBaselineIdx} labelMode={labelMode} diffFilter={diffFilter} onDiffFilterChange={setDiffFilter} testNameFilter={testNameFilter} zoomRange={sharedZoom ? chartZoom : undefined} onZoomChange={sharedZoom ? setChartZoom : undefined} chartType={chartType} />
      )}

      <BlockLogsComparison runs={runs} blockLogsPerRun={blockLogsPerRun} blockLogsLoading={blockLogsLoading} suiteTests={suite?.tests} labelMode={labelMode} testNameFilter={testNameFilter} />

      {allResults && <ResourceComparisonCharts runs={runs} labelMode={labelMode} testNameFilter={testNameFilter} suiteTests={suite?.tests} zoomRange={sharedZoom ? chartZoom : undefined} onZoomChange={sharedZoom ? setChartZoom : undefined} chartType={chartType} />}

      {allResults && (
        <TestComparisonTable runs={runs} suiteTests={suite?.tests} stepFilter={stepFilter} blockLogsPerRun={blockLogsPerRun} labelMode={labelMode} tableBaseline={tableBaseline} onTableBaselineChange={setTableBaseline} sortBy={tableSortBy} sortDir={tableSortDir} onSortChange={setTableSort} testNameFilter={testNameFilter} />
      )}

      <ConfigDiff runs={runs} labelMode={labelMode} />
    </div>
  )
}
