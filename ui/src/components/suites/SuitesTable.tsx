import { useMemo } from 'react'
import { useNavigate } from '@tanstack/react-router'
import clsx from 'clsx'
import { useSuite } from '@/api/hooks/useSuite'
import { Badge } from '@/components/shared/Badge'
import { SourceBadge } from '@/components/shared/SourceBadge'
import { formatTimestamp, formatRelativeTime } from '@/utils/date'

export type SuiteSortColumn = 'lastRun' | 'hash' | 'runs'
export type SuiteSortDirection = 'asc' | 'desc'

interface SuiteEntry {
  hash: string
  runCount: number
  lastRun: number
}

interface SuitesTableProps {
  suites: SuiteEntry[]
  sortBy?: SuiteSortColumn
  sortDir?: SuiteSortDirection
  onSortChange?: (column: SuiteSortColumn, direction: SuiteSortDirection) => void
}

function SortIcon({ direction, active }: { direction: SuiteSortDirection; active: boolean }) {
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
  column: SuiteSortColumn
  currentSort: SuiteSortColumn
  currentDirection: SuiteSortDirection
  onSort: (column: SuiteSortColumn) => void
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

function StaticHeader({ label }: { label: string }) {
  return (
    <th className="px-6 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
      {label}
    </th>
  )
}

function SuiteRow({ suite }: { suite: SuiteEntry }) {
  const navigate = useNavigate()
  const { data: suiteInfo } = useSuite(suite.hash)

  return (
    <tr
      onClick={() => navigate({ to: '/suites/$suiteHash', params: { suiteHash: suite.hash } })}
      className="cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50"
    >
      <td className="whitespace-nowrap px-6 py-4 text-sm/6 text-gray-500 dark:text-gray-400">
        <span title={formatRelativeTime(suite.lastRun)}>{formatTimestamp(suite.lastRun)}</span>
      </td>
      <td className="whitespace-nowrap px-6 py-4">
        <span className="font-mono text-sm/6 font-medium text-blue-600 dark:text-blue-400">
          {suite.hash}
        </span>
      </td>
      <td className="whitespace-nowrap px-6 py-4">
        {suiteInfo?.source.tests ? (
          <div className="flex items-center gap-2">
            <SourceBadge source={suiteInfo.source.tests} />
            <Badge variant="default">{suiteInfo.tests.length}</Badge>
          </div>
        ) : (
          <span className="text-gray-400 dark:text-gray-500">-</span>
        )}
      </td>
      <td className="whitespace-nowrap px-6 py-4">
        {suiteInfo?.source.warmup && suiteInfo.warmup ? (
          <div className="flex items-center gap-2">
            <SourceBadge source={suiteInfo.source.warmup} />
            <Badge variant="default">{suiteInfo.warmup.length}</Badge>
          </div>
        ) : (
          <span className="text-gray-400 dark:text-gray-500">-</span>
        )}
      </td>
      <td className="whitespace-nowrap px-6 py-4">
        {suiteInfo?.filter ? (
          <span className="font-mono text-sm/6 text-gray-700 dark:text-gray-300">{suiteInfo.filter}</span>
        ) : (
          <span className="text-gray-400 dark:text-gray-500">-</span>
        )}
      </td>
      <td className="whitespace-nowrap px-6 py-4">
        <Badge variant="info">{suite.runCount} runs</Badge>
      </td>
    </tr>
  )
}

export function SuitesTable({
  suites,
  sortBy = 'lastRun',
  sortDir = 'desc',
  onSortChange,
}: SuitesTableProps) {
  const handleSort = (column: SuiteSortColumn) => {
    if (onSortChange) {
      const newDirection = sortBy === column && sortDir === 'desc' ? 'asc' : 'desc'
      onSortChange(column, column === sortBy ? newDirection : 'desc')
    }
  }

  const sortedSuites = useMemo(() => {
    return [...suites].sort((a, b) => {
      let comparison = 0
      switch (sortBy) {
        case 'lastRun':
          comparison = a.lastRun - b.lastRun
          break
        case 'hash':
          comparison = a.hash.localeCompare(b.hash)
          break
        case 'runs':
          comparison = a.runCount - b.runCount
          break
      }
      return sortDir === 'asc' ? comparison : -comparison
    })
  }, [suites, sortBy, sortDir])

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
        <thead className="bg-gray-50 dark:bg-gray-900">
          <tr>
            <SortableHeader label="Last Run" column="lastRun" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <SortableHeader label="Suite Hash" column="hash" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <StaticHeader label="Tests Source" />
            <StaticHeader label="Warmup Source" />
            <StaticHeader label="Filter" />
            <SortableHeader label="Runs" column="runs" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
          {sortedSuites.map((suite) => (
            <SuiteRow key={suite.hash} suite={suite} />
          ))}
        </tbody>
      </table>
    </div>
  )
}
