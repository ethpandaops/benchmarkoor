import { useNavigate } from '@tanstack/react-router'
import type { IndexEntry } from '@/api/types'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { Badge } from '@/components/shared/Badge'
import { Duration } from '@/components/shared/Duration'
import { formatTimestamp, formatRelativeTime } from '@/utils/date'

interface RunsTableProps {
  entries: IndexEntry[]
  showSuite?: boolean
}

export function RunsTable({ entries, showSuite = true }: RunsTableProps) {
  const navigate = useNavigate()

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
        <thead className="bg-gray-50 dark:bg-gray-900">
          <tr>
            <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
              Client
            </th>
            <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
              Image
            </th>
            {showSuite && (
              <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Suite
              </th>
            )}
            <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
              Tests
            </th>
            <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
              Duration
            </th>
            <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
              Timestamp
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
          {entries.map((entry) => (
            <tr
              key={entry.run_id}
              onClick={() => navigate({ to: '/runs/$runId', params: { runId: entry.run_id } })}
              className="cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50"
            >
              <td className="whitespace-nowrap px-6 py-4">
                <ClientBadge client={entry.instance.client} />
              </td>
              <td className="max-w-xs truncate px-6 py-4 font-mono text-sm/6 text-gray-500 dark:text-gray-400">
                <span title={entry.instance.image}>{entry.instance.image}</span>
              </td>
              {showSuite && (
                <td className="whitespace-nowrap px-6 py-4 font-mono text-sm/6 text-gray-500 dark:text-gray-400">
                  {entry.suite_hash ?? '-'}
                </td>
              )}
              <td className="whitespace-nowrap px-6 py-4">
                <div className="flex items-center gap-2">
                  <Badge variant="success">{entry.tests.success} passed</Badge>
                  {entry.tests.fail > 0 && <Badge variant="error">{entry.tests.fail} failed</Badge>}
                </div>
              </td>
              <td className="whitespace-nowrap px-6 py-4 text-sm/6 text-gray-500 dark:text-gray-400">
                <Duration nanoseconds={entry.tests.duration} />
              </td>
              <td className="whitespace-nowrap px-6 py-4 text-sm/6 text-gray-500 dark:text-gray-400">
                <span title={formatTimestamp(entry.timestamp)}>{formatRelativeTime(entry.timestamp)}</span>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
