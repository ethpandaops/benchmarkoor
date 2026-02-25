import { Link, useSearch } from '@tanstack/react-router'
import { type IndexStepType, ALL_INDEX_STEP_TYPES } from '@/api/types'
import { useRunConfig } from '@/api/hooks/useRunConfig'
import { useRunResult } from '@/api/hooks/useRunResult'
import { useSuite } from '@/api/hooks/useSuite'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { JDenticon } from '@/components/shared/JDenticon'
import { CompareHeader } from '@/components/compare/CompareHeader'
import { MetricsComparison } from '@/components/compare/MetricsComparison'
import { TestComparisonTable } from '@/components/compare/TestComparisonTable'
import { ResourceComparisonCharts } from '@/components/compare/ResourceComparisonCharts'
import { ConfigDiff } from '@/components/compare/ConfigDiff'
import { type StepTypeOption, ALL_STEP_TYPES, DEFAULT_STEP_FILTER } from '@/pages/RunDetailPage'

function parseStepFilter(param: string | undefined): StepTypeOption[] {
  if (!param) return DEFAULT_STEP_FILTER
  const steps = param.split(',').filter((s): s is StepTypeOption => ALL_STEP_TYPES.includes(s as StepTypeOption))
  return steps.length > 0 ? steps : DEFAULT_STEP_FILTER
}

export function ComparePage() {
  const search = useSearch({ from: '/compare' }) as {
    a?: string
    b?: string
    steps?: string
  }
  const { a: runIdA, b: runIdB, steps } = search

  const stepFilter = parseStepFilter(steps)
  const indexStepFilter: IndexStepType[] = stepFilter.filter(
    (s): s is IndexStepType => ALL_INDEX_STEP_TYPES.includes(s as IndexStepType),
  )
  // Keep linter happy — indexStepFilter is available for future use
  void indexStepFilter

  const { data: configA, isLoading: loadingConfigA, error: errorConfigA } = useRunConfig(runIdA ?? '')
  const { data: configB, isLoading: loadingConfigB, error: errorConfigB } = useRunConfig(runIdB ?? '')
  const { data: resultA, isLoading: loadingResultA } = useRunResult(runIdA ?? '')
  const { data: resultB, isLoading: loadingResultB } = useRunResult(runIdB ?? '')

  const suiteHash = configA?.suite_hash ?? configB?.suite_hash
  const { data: suite } = useSuite(suiteHash)

  if (!runIdA || !runIdB) {
    return <ErrorState message="Both run IDs (a and b) are required. Use /compare?a={runId}&b={runId}" />
  }

  const isLoading = loadingConfigA || loadingConfigB || loadingResultA || loadingResultB
  const error = errorConfigA || errorConfigB

  if (isLoading) {
    return <LoadingState message="Loading runs for comparison..." />
  }

  if (error) {
    return <ErrorState message={error.message} />
  }

  if (!configA) {
    return <ErrorState message={`Run A not found: ${runIdA}`} />
  }

  if (!configB) {
    return <ErrorState message={`Run B not found: ${runIdB}`} />
  }

  const suiteMismatch = configA.suite_hash && configB.suite_hash && configA.suite_hash !== configB.suite_hash

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

      <CompareHeader configA={configA} configB={configB} runIdA={runIdA} runIdB={runIdB} />

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

      <MetricsComparison
        configA={configA}
        configB={configB}
        resultA={resultA ?? null}
        resultB={resultB ?? null}
        stepFilter={stepFilter}
      />

      {resultA && resultB && (
        <TestComparisonTable
          resultA={resultA}
          resultB={resultB}
          suiteTests={suite?.tests}
          stepFilter={stepFilter}
        />
      )}

      {resultA && resultB && (
        <ResourceComparisonCharts
          testsA={resultA.tests}
          testsB={resultB.tests}
        />
      )}

      <ConfigDiff configA={configA} configB={configB} />
    </div>
  )
}
