import { useEffect } from 'react'
import { Link, useSearch, useNavigate } from '@tanstack/react-router'
import { useQueries } from '@tanstack/react-query'
import { type IndexStepType, ALL_INDEX_STEP_TYPES } from '@/api/types'
import type { RunConfig, RunResult } from '@/api/types'
import { fetchData } from '@/api/client'
import { useSuite } from '@/api/hooks/useSuite'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { JDenticon } from '@/components/shared/JDenticon'
import { CompareHeader } from '@/components/compare/CompareHeader'
import { MetricsComparison } from '@/components/compare/MetricsComparison'
import { MGasComparisonChart } from '@/components/compare/MGasComparisonChart'
import { TestComparisonTable } from '@/components/compare/TestComparisonTable'
import { ResourceComparisonCharts } from '@/components/compare/ResourceComparisonCharts'
import { ConfigDiff } from '@/components/compare/ConfigDiff'
import { type StepTypeOption, ALL_STEP_TYPES, DEFAULT_STEP_FILTER } from '@/pages/RunDetailPage'
import { MIN_COMPARE_RUNS, MAX_COMPARE_RUNS, type CompareRun } from '@/components/compare/constants'

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

  const suiteHash = configQueries.find((q) => q.data?.suite_hash)?.data?.suite_hash
  const { data: suite } = useSuite(suiteHash)

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
      {/* Breadcrumb */}
      <div className="flex items-center gap-2 text-sm/6 text-gray-500 dark:text-gray-400">
        <Link to="/runs" className="hover:text-gray-700 dark:hover:text-gray-300">
          Runs
        </Link>
        <span>/</span>
        {suiteHash && (
          <>
            <Link
              to="/suites/$suiteHash"
              params={{ suiteHash }}
              className={`flex items-center gap-1.5 hover:text-gray-700 dark:hover:text-gray-300${suite?.metadata?.labels?.name ? '' : ' font-mono'}`}
            >
              <JDenticon value={suiteHash} size={16} className="shrink-0 rounded-xs" />
              {suite?.metadata?.labels?.name ?? suiteHash}
            </Link>
            <span>/</span>
          </>
        )}
        <span className="text-gray-900 dark:text-gray-100">Compare</span>
      </div>

      {suiteMismatch && (
        <div className="rounded-sm border border-yellow-300 bg-yellow-50 p-3 text-sm/6 text-yellow-800 dark:border-yellow-700 dark:bg-yellow-900/20 dark:text-yellow-300">
          Warning: These runs belong to different suites. Test-level comparison may not be meaningful.
        </div>
      )}

      <CompareHeader runs={runs} />

      <div className="flex items-center gap-2">
        <span className="text-sm/6 font-medium text-gray-700 dark:text-gray-300">Metric steps:</span>
        <div className="flex items-center gap-1">
          {ALL_INDEX_STEP_TYPES.map((step) => (
            <span
              key={step}
              className={`rounded-sm px-2.5 py-1 text-xs font-medium capitalize ${
                stepFilter.includes(step)
                  ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300'
                  : 'bg-gray-100 text-gray-400 dark:bg-gray-700 dark:text-gray-500'
              }`}
            >
              {step}
            </span>
          ))}
        </div>
      </div>

      <MetricsComparison runs={runs} stepFilter={stepFilter} />

      {allResults && (
        <MGasComparisonChart runs={runs} suiteTests={suite?.tests} stepFilter={stepFilter} />
      )}

      {allResults && (
        <TestComparisonTable runs={runs} suiteTests={suite?.tests} stepFilter={stepFilter} />
      )}

      {allResults && <ResourceComparisonCharts runs={runs} />}

      <ConfigDiff runs={runs} />
    </div>
  )
}
