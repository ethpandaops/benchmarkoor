import type { MempoolPoolLimits, RunConfig } from '@/api/types'
import { useMempoolResult } from '@/api/hooks/useMempoolResult'
import { ClientStat } from '@/components/shared/ClientStat'
import { formatDurationSeconds, formatTimestamp } from '@/utils/date'
import { summarizeMempoolResult } from './mempoolUtils'

interface MempoolSummaryProps {
  runId: string
  config: RunConfig
}

const POOL_LIMIT_LABELS: [keyof MempoolPoolLimits, string][] = [
  ['account_slots', 'Account slots'],
  ['global_slots', 'Global slots'],
  ['account_queue', 'Account queue'],
  ['global_queue', 'Global queue'],
  ['price_bump', 'Price bump %'],
  ['blob_price_bump', 'Blob price bump %'],
]

function StatTile({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
      <p className="text-sm/6 font-medium text-gray-500 dark:text-gray-400">{title}</p>
      {children}
    </div>
  )
}

function Runtime({ config }: { config: RunConfig }) {
  return (
    <>
      <p className="mt-2 text-xs/5 text-gray-500 dark:text-gray-400">Started at</p>
      <p className="text-xs/5 text-gray-900 dark:text-gray-100">{formatTimestamp(config.timestamp)}</p>
      {config.timestamp_end != null && config.timestamp_end > 0 && (
        <>
          <p className="mt-1 text-xs/5 text-gray-500 dark:text-gray-400">Total runtime</p>
          <p className="text-xs/5 text-gray-900 dark:text-gray-100">
            {formatDurationSeconds(config.timestamp_end - config.timestamp)}
          </p>
        </>
      )}
    </>
  )
}

// MempoolSummary is the stat row of a mempool run. It takes the place of
// the Tests / MGas/s / Calls / Duration row of a replay run.
export function MempoolSummary({ runId, config }: MempoolSummaryProps) {
  const { data: result, isLoading } = useMempoolResult(runId)
  const counts = result ? summarizeMempoolResult(result) : undefined

  return (
    <div className="grid grid-cols-2 gap-4 sm:grid-cols-5">
      <ClientStat client={config.instance.client} runId={config.instance.id} rollbackStrategy={config.instance.rollback_strategy} />
      {!result || !counts ? (
        <div className="col-span-1 rounded-sm bg-white p-4 shadow-xs sm:col-span-4 dark:bg-gray-800">
          <p className="text-sm/6 font-medium text-gray-500 dark:text-gray-400">Mempool results</p>
          <p className="mt-1 text-sm/6 text-gray-500 dark:text-gray-400">
            {isLoading
              ? 'Loading mempool-result.json...'
              : 'No mempool-result.json available. The run may still be in progress or may have failed before producing results.'}
          </p>
          <Runtime config={config} />
        </div>
      ) : (
        <>
          <StatTile title="Scenarios">
            <p className="mt-1 flex items-center gap-2 text-2xl/8 font-semibold tabular-nums">
              <span className="text-gray-900 dark:text-gray-100">{counts.total}</span>
              <span className="text-gray-400 dark:text-gray-500">/</span>
              <span className="text-green-600 dark:text-green-400" title="Passed">{counts.passed}</span>
              {counts.failed > 0 && (
                <>
                  <span className="text-gray-400 dark:text-gray-500">/</span>
                  <span className="text-red-600 dark:text-red-400" title="Failed">{counts.failed}</span>
                </>
              )}
              {counts.error > 0 && (
                <>
                  <span className="text-gray-400 dark:text-gray-500">/</span>
                  <span className="text-yellow-600 dark:text-yellow-400" title="Error">{counts.error}</span>
                </>
              )}
            </p>
            <p className="text-xs/5 text-gray-500 dark:text-gray-400">
              {counts.passed} passed, {counts.failed} failed, {counts.error} error
            </p>
            <Runtime config={config} />
          </StatTile>
          <StatTile title="Findings">
            <p className="mt-1 flex items-baseline gap-3 text-2xl/8 font-semibold tabular-nums">
              <span
                className={counts.findings > 0 ? 'text-amber-600 dark:text-amber-400' : 'text-gray-900 dark:text-gray-100'}
                title="Soft checks that did not pass"
              >
                {counts.findings}
              </span>
              {counts.hardFailures > 0 && (
                <span className="text-base/7 font-medium text-red-600 dark:text-red-400">
                  {counts.hardFailures} hard failure{counts.hardFailures === 1 ? '' : 's'}
                </span>
              )}
            </p>
            <p className="text-xs/5 text-gray-500 dark:text-gray-400">
              Soft checks that did not pass. Findings do not fail a scenario.
            </p>
            <p className="mt-2 text-xs/5 text-gray-500 dark:text-gray-400">
              {counts.observations} observation{counts.observations === 1 ? '' : 's'} to compare between clients
            </p>
          </StatTile>
          <StatTile title="Chain">
            <dl className="mt-1 flex flex-col gap-1 text-xs/5">
              <div className="flex justify-between gap-2">
                <dt className="text-gray-500 dark:text-gray-400">Fork</dt>
                <dd className="font-medium text-gray-900 dark:text-gray-100">{result.fork}</dd>
              </div>
              <div className="flex justify-between gap-2">
                <dt className="text-gray-500 dark:text-gray-400">Chain ID</dt>
                <dd className="font-mono text-gray-900 dark:text-gray-100">{result.chain_id}</dd>
              </div>
              <div className="flex justify-between gap-2">
                <dt className="text-gray-500 dark:text-gray-400">Instance</dt>
                <dd className="truncate font-mono text-gray-900 dark:text-gray-100" title={result.instance_id}>{result.instance_id}</dd>
              </div>
            </dl>
          </StatTile>
          <StatTile title="Pool limits">
            <dl className="mt-1 flex flex-col gap-0.5 text-xs/5">
              {POOL_LIMIT_LABELS.map(([key, label]) => (
                <div key={key} className="flex justify-between gap-2">
                  <dt className="text-gray-500 dark:text-gray-400">{label}</dt>
                  <dd className="font-mono text-gray-900 dark:text-gray-100">{result.pool_limits?.[key]?.toLocaleString() ?? '-'}</dd>
                </div>
              ))}
            </dl>
          </StatTile>
        </>
      )}
    </div>
  )
}
