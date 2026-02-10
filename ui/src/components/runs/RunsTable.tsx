import { useMemo } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import clsx from 'clsx'
import { type IndexEntry, type IndexStepType, ALL_INDEX_STEP_TYPES, getIndexAggregatedStats } from '@/api/types'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { Badge } from '@/components/shared/Badge'
import { StatusBadge } from '@/components/shared/StatusBadge'
import { Duration } from '@/components/shared/Duration'
import { JDenticon } from '@/components/shared/JDenticon'
import { formatTimestamp, formatRelativeTime } from '@/utils/date'

// Calculates MGas/s from gas_used and gas_used_duration
function calculateMGasPerSec(gasUsed: number, gasUsedDuration: number): number | undefined {
  if (gasUsedDuration <= 0 || gasUsed <= 0) return undefined
  return (gasUsed * 1000) / gasUsedDuration
}


export type SortColumn = 'timestamp' | 'client' | 'image' | 'suite' | 'duration' | 'mgas' | 'failed' | 'passed' | 'total'
export type SortDirection = 'asc' | 'desc'

interface RunsTableProps {
  entries: IndexEntry[]
  sortBy?: SortColumn
  sortDir?: SortDirection
  onSortChange?: (column: SortColumn, direction: SortDirection) => void
  showSuite?: boolean
  stepFilter?: IndexStepType[]
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
  className,
}: {
  label: string
  column: SortColumn
  currentSort: SortColumn
  currentDirection: SortDirection
  onSort: (column: SortColumn) => void
  className?: string
}) {
  const isActive = currentSort === column
  return (
    <th
      onClick={() => onSort(column)}
      className={clsx('cursor-pointer select-none text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300', className ?? 'px-6 py-3')}
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
  stepFilter = ALL_INDEX_STEP_TYPES,
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
      const statsA = getIndexAggregatedStats(a, stepFilter)
      const statsB = getIndexAggregatedStats(b, stepFilter)
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
          comparison = statsA.duration - statsB.duration
          break
        case 'mgas': {
          const mgasA = calculateMGasPerSec(statsA.gasUsed, statsA.gasUsedDuration) ?? -Infinity
          const mgasB = calculateMGasPerSec(statsB.gasUsed, statsB.gasUsedDuration) ?? -Infinity
          comparison = mgasA - mgasB
          break
        }
        case 'failed':
          comparison = (a.tests.tests_total - a.tests.tests_passed) - (b.tests.tests_total - b.tests.tests_passed)
          break
        case 'passed':
          comparison = a.tests.tests_passed - b.tests.tests_passed
          break
        case 'total':
          comparison = a.tests.tests_total - b.tests.tests_total
          break
      }
      return sortDir === 'asc' ? comparison : -comparison
    })
  }, [entries, sortBy, sortDir, stepFilter])

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
        <thead className="bg-gray-50 dark:bg-gray-900">
          <tr>
            <SortableHeader label="Timestamp" column="timestamp" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <SortableHeader label="Client" column="client" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <SortableHeader label="Image" column="image" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            {showSuite && <SortableHeader label="Suite" column="suite" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />}
            <SortableHeader label="MGas/s" column="mgas" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <SortableHeader label="Duration" column="duration" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <SortableHeader label="F" column="failed" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="px-2 py-3" />
            <SortableHeader label="P" column="passed" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="px-2 py-3" />
            <SortableHeader label="T" column="total" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="px-2 py-3" />
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
          {sortedEntries.map((entry) => (
            <tr
              key={entry.run_id}
              onClick={() => navigate({ to: '/runs/$runId', params: { runId: entry.run_id } })}
              className={clsx(
                'cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50',
                entry.status === 'container_died' && 'bg-red-50/50 dark:bg-red-900/10',
                entry.status === 'cancelled' && 'bg-yellow-50/50 dark:bg-yellow-900/10',
              )}
            >
              <td className="whitespace-nowrap px-6 py-4 text-sm/6 text-gray-500 dark:text-gray-400">
                <div className="flex items-center gap-2">
                  <span title={formatRelativeTime(entry.timestamp)}>{formatTimestamp(entry.timestamp)}</span>
                  <StatusBadge status={entry.status} compact />
                </div>
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
                        title={entry.suite_hash}
                      >
                        {entry.suite_hash.slice(0, 4)}
                      </Link>
                    </div>
                  ) : (
                    <span className="text-gray-400 dark:text-gray-500">-</span>
                  )}
                </td>
              )}
              {(() => {
                const stats = getIndexAggregatedStats(entry, stepFilter)
                return (
                  <>
                    <td className="whitespace-nowrap px-6 py-4 text-right text-sm/6 text-gray-500 dark:text-gray-400">
                      {(() => {
                        const mgas = calculateMGasPerSec(stats.gasUsed, stats.gasUsedDuration)
                        return mgas !== undefined ? mgas.toFixed(2) : '-'
                      })()}
                    </td>
                    <td className="whitespace-nowrap px-6 py-4 text-right text-sm/6 text-gray-500 dark:text-gray-400">
                      <Duration nanoseconds={stats.duration} />
                    </td>
                    <td className="whitespace-nowrap px-2 py-4 text-center">
                      {entry.tests.tests_total - entry.tests.tests_passed > 0 && (
                        <Badge variant="error">{entry.tests.tests_total - entry.tests.tests_passed}</Badge>
                      )}
                    </td>
                    <td className="whitespace-nowrap px-2 py-4 text-center">
                      <Badge variant="success">{entry.tests.tests_passed}</Badge>
                    </td>
                    <td className="whitespace-nowrap px-2 py-4 text-center">
                      <Badge>{entry.tests.tests_total}</Badge>
                    </td>
                  </>
                )
              })()}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
