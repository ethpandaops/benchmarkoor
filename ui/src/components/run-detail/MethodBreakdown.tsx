import type { MethodStats } from '@/api/types'
import { Duration } from '@/components/shared/Duration'
import { formatNumber } from '@/utils/format'
import { ExecutionsList } from './ExecutionsList'

interface MethodBreakdownProps {
  methods: Record<string, MethodStats>
  runId: string
  suiteHash?: string
  testName: string
  dir?: string
}

export function MethodBreakdown({ methods, runId, suiteHash, testName, dir }: MethodBreakdownProps) {
  const methodEntries = Object.entries(methods).sort(([a], [b]) => a.localeCompare(b))

  if (methodEntries.length === 0) {
    return <p className="text-sm/6 text-gray-500 dark:text-gray-400">No method data available</p>
  }

  return (
    <div className="flex flex-col gap-4">
      <h4 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Method Breakdown</h4>
      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
          <thead>
            <tr>
              <th className="px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Method
              </th>
              <th className="px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Count
              </th>
              <th className="px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Last
              </th>
              {methodEntries.some(([, stats]) => stats.min !== undefined) && (
                <>
                  <th className="px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    Min
                  </th>
                  <th className="px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    Max
                  </th>
                  <th className="px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    Mean
                  </th>
                </>
              )}
              {methodEntries.some(([, stats]) => stats.p50 !== undefined) && (
                <>
                  <th className="px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    P50
                  </th>
                  <th className="px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                    P95
                  </th>
                  <th className="px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
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
                <td className="whitespace-nowrap px-3 py-2 text-sm/6 text-gray-500 dark:text-gray-400">
                  {formatNumber(stats.count)}
                </td>
                <td className="whitespace-nowrap px-3 py-2 text-sm/6 text-gray-500 dark:text-gray-400">
                  <Duration nanoseconds={stats.last} />
                </td>
                {stats.min !== undefined && (
                  <>
                    <td className="whitespace-nowrap px-3 py-2 text-sm/6 text-gray-500 dark:text-gray-400">
                      <Duration nanoseconds={stats.min} />
                    </td>
                    <td className="whitespace-nowrap px-3 py-2 text-sm/6 text-gray-500 dark:text-gray-400">
                      <Duration nanoseconds={stats.max!} />
                    </td>
                    <td className="whitespace-nowrap px-3 py-2 text-sm/6 text-gray-500 dark:text-gray-400">
                      <Duration nanoseconds={stats.mean!} />
                    </td>
                  </>
                )}
                {stats.p50 !== undefined && (
                  <>
                    <td className="whitespace-nowrap px-3 py-2 text-sm/6 text-gray-500 dark:text-gray-400">
                      <Duration nanoseconds={stats.p50} />
                    </td>
                    <td className="whitespace-nowrap px-3 py-2 text-sm/6 text-gray-500 dark:text-gray-400">
                      <Duration nanoseconds={stats.p95!} />
                    </td>
                    <td className="whitespace-nowrap px-3 py-2 text-sm/6 text-gray-500 dark:text-gray-400">
                      <Duration nanoseconds={stats.p99!} />
                    </td>
                  </>
                )}
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {suiteHash && (
        <ExecutionsList runId={runId} suiteHash={suiteHash} testName={testName} dir={dir} />
      )}
    </div>
  )
}
