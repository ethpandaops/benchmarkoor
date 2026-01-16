import type { MethodStatsFloat } from '@/api/types'
import { formatNumber } from '@/utils/format'

interface MGasBreakdownProps {
  methods: Record<string, MethodStatsFloat>
}

function formatMGas(value: number): string {
  return value.toFixed(2)
}

export function MGasBreakdown({ methods }: MGasBreakdownProps) {
  const methodEntries = Object.entries(methods).sort(([a], [b]) => a.localeCompare(b))

  if (methodEntries.length === 0) {
    return null
  }

  return (
    <div className="flex flex-col gap-4">
      <h4 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">MGas/s Breakdown</h4>
      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
          <thead>
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
          <tbody className="divide-y divide-gray-100 dark:divide-gray-800">
            {methodEntries.map(([method, stats]) => (
              <tr key={method}>
                <td className="whitespace-nowrap px-3 py-2 font-mono text-sm/6 text-gray-900 dark:text-gray-100">
                  {method}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-right text-sm/6 text-gray-500 dark:text-gray-400">
                  {formatNumber(stats.count)}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-right text-sm/6 text-blue-600 dark:text-blue-400">
                  {formatMGas(stats.last)}
                </td>
                {stats.min !== undefined && (
                  <>
                    <td className="whitespace-nowrap px-3 py-2 text-right text-sm/6 text-blue-600 dark:text-blue-400">
                      {formatMGas(stats.min)}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2 text-right text-sm/6 text-blue-600 dark:text-blue-400">
                      {formatMGas(stats.max!)}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2 text-right text-sm/6 text-blue-600 dark:text-blue-400">
                      {formatMGas(stats.mean!)}
                    </td>
                  </>
                )}
                {stats.p50 !== undefined && (
                  <>
                    <td className="whitespace-nowrap px-3 py-2 text-right text-sm/6 text-blue-600 dark:text-blue-400">
                      {formatMGas(stats.p50)}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2 text-right text-sm/6 text-blue-600 dark:text-blue-400">
                      {formatMGas(stats.p95!)}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2 text-right text-sm/6 text-blue-600 dark:text-blue-400">
                      {formatMGas(stats.p99!)}
                    </td>
                  </>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}
