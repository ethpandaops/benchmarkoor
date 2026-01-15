import { Link, useParams } from '@tanstack/react-router'
import { useSuite } from '@/api/hooks/useSuite'
import { SuiteSource } from '@/components/suite-detail/SuiteSource'
import { TestFilesList } from '@/components/suite-detail/TestFilesList'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { Badge } from '@/components/shared/Badge'

export function SuiteDetailPage() {
  const { suiteHash } = useParams({ from: '/suites/$suiteHash' })
  const { data: suite, isLoading, error, refetch } = useSuite(suiteHash)

  if (isLoading) {
    return <LoadingState message="Loading suite details..." />
  }

  if (error) {
    return <ErrorState message={error.message} retry={() => refetch()} />
  }

  if (!suite) {
    return <ErrorState message="Suite not found" />
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
        <div className="flex flex-col gap-2">
          <h1 className="font-mono text-2xl/8 font-bold text-gray-900 dark:text-gray-100">{suite.hash}</h1>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          {suite.filter && <Badge variant="info">Filter: {suite.filter}</Badge>}
          <Badge variant="default">{suite.tests.length} tests</Badge>
          {suite.warmup && <Badge variant="warning">{suite.warmup.length} warmup</Badge>}
        </div>
      </div>

      {suite.source.tests && <SuiteSource title="Tests Source" source={suite.source.tests} />}
      {suite.source.warmup && <SuiteSource title="Warmup Source" source={suite.source.warmup} />}

      <TestFilesList title="Tests" files={suite.tests} />
      {suite.warmup && suite.warmup.length > 0 && (
        <TestFilesList title="Warmup Tests" files={suite.warmup} defaultCollapsed />
      )}
    </div>
  )
}
