import { useMemo } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import clsx from 'clsx'
import type { IndexEntry } from '@/api/types'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { Badge } from '@/components/shared/Badge'
import { Duration } from '@/components/shared/Duration'
import { JDenticon } from '@/components/shared/JDenticon'
import { formatTimestamp, formatRelativeTime } from '@/utils/date'

export type SortColumn = 'timestamp' | 'client' | 'image' | 'suite' | 'duration' | 'tests'
export type SortDirection = 'asc' | 'desc'

interface RunsTableProps {
  entries: IndexEntry[]
  sortBy?: SortColumn
  sortDir?: SortDirection
  onSortChange?: (column: SortColumn, direction: SortDirection) => void
  showSuite?: boolean
}

function SortIcon({ direction, active }: { direction: SortDirection; active: boolean }) {
  return (
    <svg
      className={clsx('ml-1 inline-block size-3', active ? 'text-gray-700 dark:text-gray-300' : 'text-gray-400')}
      viewBox="0 0 12 12"
      fill="currentColor"
    >
      {direction === 'asc' ? (
        <path d="M6 2L10 8H2L6 2Z" />
      ) : (
        <path d="M6 10L2 4H10L6 10Z" />
      )}
    </svg>
  )
}

function SortableHeader({
  label,
  column,
  currentSort,
  currentDirection,
  onSort,
}: {
  label: string
  column: SortColumn
  currentSort: SortColumn
  currentDirection: SortDirection
  onSort: (column: SortColumn) => void
}) {
  const isActive = currentSort === column
  return (
    <th
      onClick={() => onSort(column)}
      className="cursor-pointer select-none px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300"
    >
      {label}
      <SortIcon direction={isActive ? currentDirection : 'asc'} active={isActive} />
    </th>
  )
}

export function RunsTable({
  entries,
  sortBy = 'timestamp',
  sortDir = 'desc',
  onSortChange,
  showSuite = false,
}: RunsTableProps) {
  const navigate = useNavigate()

  const handleSort = (column: SortColumn) => {
    if (onSortChange) {
      const newDirection = sortBy === column && sortDir === 'desc' ? 'asc' : 'desc'
      onSortChange(column, column === sortBy ? newDirection : 'desc')
    }
  }

  const sortedEntries = useMemo(() => {
    return [...entries].sort((a, b) => {
      let comparison = 0
      switch (sortBy) {
        case 'timestamp':
          comparison = a.timestamp - b.timestamp
          break
        case 'client':
          comparison = a.instance.client.localeCompare(b.instance.client)
          break
        case 'image':
          comparison = a.instance.image.localeCompare(b.instance.image)
          break
        case 'suite':
          comparison = (a.suite_hash ?? '').localeCompare(b.suite_hash ?? '')
          break
        case 'duration':
          comparison = a.tests.duration - b.tests.duration
          break
        case 'tests':
          comparison = a.tests.success - b.tests.success
          break
      }
      return sortDir === 'asc' ? comparison : -comparison
    })
  }, [entries, sortBy, sortDir])

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
        <thead className="bg-gray-50 dark:bg-gray-900">
          <tr>
            <SortableHeader label="Timestamp" column="timestamp" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <SortableHeader label="Client" column="client" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <SortableHeader label="Image" column="image" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            {showSuite && <SortableHeader label="Suite" column="suite" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />}
            <SortableHeader label="Duration" column="duration" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <SortableHeader label="Tests" column="tests" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
          {sortedEntries.map((entry) => (
            <tr
              key={entry.run_id}
              onClick={() => navigate({ to: '/runs/$runId', params: { runId: entry.run_id } })}
              className="cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50"
            >
              <td className="whitespace-nowrap px-6 py-4 text-sm/6 text-gray-500 dark:text-gray-400">
                <span title={formatRelativeTime(entry.timestamp)}>{formatTimestamp(entry.timestamp)}</span>
              </td>
              <td className="whitespace-nowrap px-6 py-4">
                <ClientBadge client={entry.instance.client} />
              </td>
              <td className="max-w-xs truncate px-6 py-4 font-mono text-sm/6 text-gray-500 dark:text-gray-400">
                <span title={entry.instance.image}>{entry.instance.image}</span>
              </td>
              {showSuite && (
                <td className="whitespace-nowrap px-6 py-4 font-mono text-sm/6">
                  {entry.suite_hash ? (
                    <div className="flex items-center gap-2">
                      <JDenticon value={entry.suite_hash} size={20} className="shrink-0 rounded-xs" />
                      <Link
                        to="/suites/$suiteHash"
                        params={{ suiteHash: entry.suite_hash }}
                        onClick={(e) => e.stopPropagation()}
                        className="text-blue-600 hover:text-blue-800 hover:underline dark:text-blue-400 dark:hover:text-blue-300"
                      >
                        {entry.suite_hash}
                      </Link>
                    </div>
                  ) : (
                    <span className="text-gray-400 dark:text-gray-500">-</span>
                  )}
                </td>
              )}
              <td className="whitespace-nowrap px-6 py-4 text-sm/6 text-gray-500 dark:text-gray-400">
                <Duration nanoseconds={entry.tests.duration} />
              </td>
              <td className="whitespace-nowrap px-6 py-4">
                <div className="flex items-center gap-2">
                  <Badge variant="success">{entry.tests.success} passed</Badge>
                  {entry.tests.fail > 0 && <Badge variant="error">{entry.tests.fail} failed</Badge>}
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
