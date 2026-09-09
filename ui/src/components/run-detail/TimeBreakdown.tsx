import clsx from 'clsx'
import type { MethodStats } from '@/api/types'
import { Duration } from '@/components/shared/Duration'
import { formatDuration, formatNumber } from '@/utils/format'
import { DEFAULT_SLOW_MS, formatSlowMs, getTextClassByDuration } from '@/utils/perfThreshold'

interface TimeBreakdownProps {
  methods: Record<string, MethodStats>
  /** Slow-payload limit in milliseconds the payload times colour against. */
  slowMs?: number
}

// Engine API method that submits a block. Only its times mean anything
// against the slow-payload limit, so only they carry a colour.
const PAYLOAD_METHOD = 'newPayload'

export function TimeBreakdown({ methods, slowMs = DEFAULT_SLOW_MS }: TimeBreakdownProps) {
  const methodEntries = Object.entries(methods).sort(([a], [b]) => a.localeCompare(b))

  if (methodEntries.length === 0) {
    return <p className="text-sm/6 text-gray-500 dark:text-gray-400">No method data available</p>
  }

  const hasPayload = methodEntries.some(([method]) => method.includes(PAYLOAD_METHOD))

  // A payload time carries the colour of the step it falls in. Every
  // other method stays grey — a forkchoice call has nothing to do with
  // the slow-payload limit.
  const cell = (nanoseconds: number, isPayload: boolean) => (
    <td
      className={clsx(
        'whitespace-nowrap px-3 py-2 text-right text-sm/6',
        isPayload ? getTextClassByDuration(nanoseconds, slowMs) : 'text-gray-500 dark:text-gray-400',
      )}
      title={isPayload ? `${formatDuration(nanoseconds)} — slow-payload limit ${formatSlowMs(slowMs)}` : undefined}
    >
      <Duration nanoseconds={nanoseconds} />
    </td>
  )

  return (
    <div className="overflow-hidden rounded-sm border border-gray-200 dark:border-gray-700">
      <div className="border-b border-gray-200 bg-gray-50 px-4 py-2 dark:border-gray-700 dark:bg-gray-800/50">
        <h4 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">
          RPC Calls Time Breakdown
          {hasPayload && (
            <span className="ml-2 text-xs/5 font-normal text-gray-500 dark:text-gray-400">
              payload times coloured against {formatSlowMs(slowMs)}
            </span>
          )}
        </h4>
      </div>
      <div className="overflow-x-auto">
        <table className="min-w-full">
          <thead className="bg-gray-50/50 dark:bg-gray-800/30">
            <tr>
              <th className="px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Method
              </th>
              <th className="px-3 py-2 text-right text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Count
              </th>
              <th className="px-3 py-2 text-right text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Last
              </th>
              {methodEntries.some(([, stats]) => stats.min !== undefined) && (
                <>
                  <th className="px-3 py-2 text-right text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    Min
                  </th>
                  <th className="px-3 py-2 text-right text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    Max
                  </th>
                  <th className="px-3 py-2 text-right text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    Mean
                  </th>
                </>
              )}
              {methodEntries.some(([, stats]) => stats.p50 !== undefined) && (
                <>
                  <th className="px-3 py-2 text-right text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    P50
                  </th>
                  <th className="px-3 py-2 text-right text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    P95
                  </th>
                  <th className="px-3 py-2 text-right text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    P99
                  </th>
                </>
              )}
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100 bg-white dark:divide-gray-800 dark:bg-gray-900/20">
            {methodEntries.map(([method, stats]) => {
              const isPayload = method.includes(PAYLOAD_METHOD)
              return (
                <tr key={method}>
                  <td className="whitespace-nowrap px-3 py-2 font-mono text-sm/6 text-gray-900 dark:text-gray-100">
                    {method}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-right text-sm/6 text-gray-500 dark:text-gray-400">
                    {formatNumber(stats.count)}
                  </td>
                  {cell(stats.last, isPayload)}
                  {stats.min !== undefined && (
                    <>
                      {cell(stats.min, isPayload)}
                      {cell(stats.max!, isPayload)}
                      {cell(stats.mean!, isPayload)}
                    </>
                  )}
                  {stats.p50 !== undefined && (
                    <>
                      {cell(stats.p50, isPayload)}
                      {cell(stats.p95!, isPayload)}
                      {cell(stats.p99!, isPayload)}
                    </>
                  )}
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
