import { useMemo } from 'react'
import clsx from 'clsx'
import type { SuiteStats, RunDuration } from '@/api/types'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { TestName } from '@/components/shared/TestName'
import { formatDuration } from '@/utils/format'

interface RejectionTimesTableProps {
  stats: SuiteStats
}

interface ClientRejections {
  // Times to reject, most recent run first.
  timesNs: number[]
  // Runs of this test that recorded no rejection (failed, or the client
  // accepted the payload).
  missed: number
}

interface RejectionRow {
  testName: string
  byClient: Record<string, ClientRejections>
}

// Invalid-payload benchmarks have no MGas/s: the client may stop part way
// through the block. This table compares their time to reject across clients.
export function RejectionTimesTable({ stats }: RejectionTimesTableProps) {
  const { rows, clients } = useMemo(() => {
    const clientSet = new Set<string>()
    const rows: RejectionRow[] = []

    for (const [testName, testDurations] of Object.entries(stats)) {
      if (!testDurations.durations.some((d) => (d.rejected_time_ns ?? 0) > 0)) continue

      const byClient: Record<string, ClientRejections> = {}
      const runs = [...testDurations.durations].sort((a: RunDuration, b: RunDuration) => b.run_start - a.run_start)
      for (const run of runs) {
        clientSet.add(run.client)
        const entry = (byClient[run.client] ??= { timesNs: [], missed: 0 })
        if ((run.rejected_time_ns ?? 0) > 0) {
          entry.timesNs.push(run.rejected_time_ns!)
        } else {
          entry.missed++
        }
      }
      rows.push({ testName, byClient })
    }

    rows.sort((a, b) => a.testName.localeCompare(b.testName))
    return { rows, clients: Array.from(clientSet).sort() }
  }, [stats])

  if (rows.length === 0) return null

  return (
    <div className="overflow-hidden rounded-sm border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
      <div className="flex flex-col gap-1 border-b border-gray-200 px-4 py-3 dark:border-gray-700">
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Invalid payloads: time to reject</h3>
        <p className="text-xs/5 text-gray-500 dark:text-gray-400">
          The client must reject these payloads, so they have no MGas/s. Each cell is the latest run&apos;s time from
          engine_newPayload to the INVALID answer. Green is the fastest client for the test.
        </p>
      </div>
      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
          <thead className="bg-gray-50 dark:bg-gray-900">
            <tr>
              <th className="px-4 py-2 text-left text-xs/5 font-medium text-gray-500 uppercase dark:text-gray-400">Test</th>
              {clients.map((client) => (
                <th key={client} className="px-4 py-2 text-right text-xs/5 font-medium text-gray-500 dark:text-gray-400">
                  <ClientBadge client={client} className="justify-end" />
                </th>
              ))}
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {rows.map((row) => {
              const latest = clients
                .map((c) => row.byClient[c]?.timesNs[0])
                .filter((ns): ns is number => ns !== undefined)
              const fastest = latest.length > 1 ? Math.min(...latest) : undefined
              return (
                <tr key={row.testName}>
                  <td className="max-w-md px-4 py-2 text-sm/6">
                    <TestName name={row.testName} variant="compact" />
                  </td>
                  {clients.map((client) => {
                    const entry = row.byClient[client]
                    const latestNs = entry?.timesNs[0]
                    if (!entry) {
                      return <td key={client} className="px-4 py-2 text-right text-sm/6 text-gray-400 dark:text-gray-500">·</td>
                    }
                    if (latestNs === undefined) {
                      return (
                        <td
                          key={client}
                          className="px-4 py-2 text-right text-sm/6 text-red-600 dark:text-red-400"
                          title="No rejection recorded: the run failed (for example, the client rejected a setup block) or the client accepted the invalid payload."
                        >
                          —
                        </td>
                      )
                    }
                    return (
                      <td
                        key={client}
                        className={clsx(
                          'px-4 py-2 text-right font-mono text-sm/6',
                          latestNs === fastest ? 'font-semibold text-green-600 dark:text-green-400' : 'text-gray-900 dark:text-gray-100',
                        )}
                        title={`Runs (latest first): ${entry.timesNs.map(formatDuration).join(', ')}${entry.missed > 0 ? ` · ${entry.missed} run(s) without a rejection` : ''}`}
                      >
                        {formatDuration(latestNs)}
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
