import { Link, useParams } from '@tanstack/react-router'
import { useRunConfig } from '@/api/hooks/useRunConfig'
import { useRunResult } from '@/api/hooks/useRunResult'
import { SystemInfo } from '@/components/run-detail/SystemInfo'
import { InstanceConfig } from '@/components/run-detail/InstanceConfig'
import { TestsTable } from '@/components/run-detail/TestsTable'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { Badge } from '@/components/shared/Badge'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { Duration } from '@/components/shared/Duration'
import { JDenticon } from '@/components/shared/JDenticon'
import { formatTimestamp } from '@/utils/date'

export function RunDetailPage() {
  const { runId } = useParams({ from: '/runs/$runId' })
  const { data: config, isLoading: configLoading, error: configError, refetch: refetchConfig } = useRunConfig(runId)
  const { data: result, isLoading: resultLoading, error: resultError, refetch: refetchResult } = useRunResult(runId)

  const isLoading = configLoading || resultLoading
  const error = configError || resultError

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
  const successCount = Object.values(result.tests).reduce((sum, t) => sum + t.aggregated.success, 0)
  const failCount = Object.values(result.tests).reduce((sum, t) => sum + t.aggregated.fail, 0)
  const totalDuration = Object.values(result.tests).reduce((sum, t) => sum + t.aggregated.time_total, 0)

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
      </div>

      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-col gap-2">
          <div className="flex items-center gap-3">
            <h1 className="text-2xl/8 font-bold text-gray-900 dark:text-gray-100">{config.instance.id}</h1>
            <ClientBadge client={config.instance.client} />
          </div>
          <p className="text-sm/6 text-gray-500 dark:text-gray-400">{formatTimestamp(config.timestamp)}</p>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <div className="flex items-center gap-2">
            <Badge variant="info">{testCount} tests</Badge>
            <Badge variant="success">{successCount} passed</Badge>
            {failCount > 0 && <Badge variant="error">{failCount} failed</Badge>}
          </div>
          <span className="text-sm/6 text-gray-500 dark:text-gray-400">
            Total: <Duration nanoseconds={totalDuration} />
          </span>
        </div>
      </div>

      <SystemInfo system={config.system} />
      <InstanceConfig instance={config.instance} />
      <TestsTable tests={result.tests} runId={runId} />
    </div>
  )
}
