import { useMemo } from 'react'
import clsx from 'clsx'
import type { MempoolRunResult } from '@/api/types'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { comparePolicies } from './policyProfile'

interface PolicyComparisonProps {
  results: { client: string; runId: string; result: MempoolRunResult }[]
  currentRunId: string
}

const CURRENT_COL = 'bg-blue-50/60 dark:bg-blue-900/15'

// PolicyComparison shows the pool policy of each client in one table: a row
// per policy field, a column per client.
export function PolicyComparison({ results, currentRunId }: PolicyComparisonProps) {
  const rows = useMemo(() => comparePolicies(results), [results])
  const differ = rows.filter((r) => r.differs).length

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Pool policy</h3>
        <p className="text-xs/5 text-gray-500 dark:text-gray-400">
          {differ} of {rows.length - 1} fields differ. Hover a field for what it means.
        </p>
      </div>
      <div className="overflow-x-auto rounded-xs border border-gray-200 dark:border-gray-700">
        <table className="w-full min-w-3xl table-fixed text-xs/5">
          <colgroup>
            <col className="w-72" />
            {results.map((r) => (
              <col key={r.client} />
            ))}
          </colgroup>
          <thead className="bg-white dark:bg-gray-800">
            <tr className="text-left font-medium text-gray-500 dark:text-gray-400">
              <th className="px-3 py-2 font-medium">Field</th>
              {results.map((r) => (
                <th key={r.client} className={clsx('px-3 py-2 text-right', r.runId === currentRunId && CURRENT_COL)}>
                  <span className="inline-flex justify-end">
                    <ClientBadge client={r.client} />
                  </span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const model = row.key === 'limit_model'
              return (
                <tr
                  key={row.key}
                  className={clsx('border-t border-gray-100 dark:border-gray-700/50', row.differs && 'bg-blue-50/40 dark:bg-blue-900/10')}
                >
                  <td title={row.hint} className="cursor-help px-3 py-1.5 text-gray-700 dark:text-gray-300">
                    {row.label}
                  </td>
                  {results.map((r) => {
                    const v = row.values[r.client]
                    return (
                      <td
                        key={r.client}
                        className={clsx(
                          'px-3 py-1.5 text-right',
                          model ? 'align-top text-gray-600 dark:text-gray-400' : 'font-mono tabular-nums',
                          !model && (v === undefined ? 'text-gray-400 dark:text-gray-500' : 'text-gray-900 dark:text-gray-100'),
                          r.runId === currentRunId && CURRENT_COL,
                        )}
                      >
                        {v ?? '—'}
                      </td>
                    )
                  })}
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
