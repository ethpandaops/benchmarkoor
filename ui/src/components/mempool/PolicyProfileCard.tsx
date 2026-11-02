import { Link } from '@tanstack/react-router'
import clsx from 'clsx'
import type { MempoolRunResult } from '@/api/types'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { buildPolicyProfile, limitModel } from './policyProfile'

interface PolicyProfileCardProps {
  client: string
  result: MempoolRunResult
  runId?: string
  // wide lays the fields out in columns, for a full-width card.
  wide?: boolean
  className?: string
}

// PolicyProfileCard is the pool policy of one client, derived from its run.
export function PolicyProfileCard({ client, result, runId, wide = false, className }: PolicyProfileCardProps) {
  const fields = buildPolicyProfile(result)
  const model = limitModel(client)
  return (
    <div className={clsx('flex flex-col gap-2 rounded-sm bg-white p-3 shadow-xs dark:bg-gray-800', className)}>
      <div className="flex min-w-0 flex-col gap-0.5">
        <span className="flex min-w-0 items-center gap-2">
          <ClientBadge client={client} />
          {wide && <span className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Pool policy</span>}
        </span>
        {runId && !wide && (
          <Link
            to="/runs/$runId"
            params={{ runId }}
            title={runId}
            className="min-w-0 truncate text-xs/5 text-gray-500 hover:text-blue-600 hover:underline dark:text-gray-400 dark:hover:text-blue-400"
          >
            {runId}
          </Link>
        )}
      </div>
      {model && (
        <p className="text-xs/5 text-gray-600 dark:text-gray-400" title="How the client limits its pool (from the client code, not measured)">
          <span className="font-medium text-gray-700 dark:text-gray-300">Limit model: </span>
          {model}
        </p>
      )}
      <dl className={clsx('grid gap-x-6 text-xs/5', wide ? 'grid-cols-1 sm:grid-cols-2 lg:grid-cols-4' : 'grid-cols-1')}>
        {fields.map((f) => (
          <div key={f.key} className="flex min-w-0 items-baseline justify-between gap-2 border-b border-gray-100 py-0.5 dark:border-gray-700/50">
            <dt title={f.hint} className="min-w-0 cursor-help truncate text-gray-500 dark:text-gray-400">
              {f.label}
            </dt>
            <dd className={clsx('shrink-0 font-mono tabular-nums', f.value === undefined ? 'text-gray-400 dark:text-gray-500' : 'text-gray-900 dark:text-gray-100')}>
              {f.value ?? '—'}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  )
}
