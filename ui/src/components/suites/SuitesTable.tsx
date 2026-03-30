import { useMemo, useState } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import clsx from 'clsx'
import { useSuite } from '@/api/hooks/useSuite'
import { Badge } from '@/components/shared/Badge'
import { JDenticon } from '@/components/shared/JDenticon'
import { SourceBadge } from '@/components/shared/SourceBadge'
import { formatTimestampDate, formatTimestampTime, formatRelativeTime } from '@/utils/date'

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
  hideInactive?: boolean
  inactiveThresholdMs?: number
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
      className="cursor-pointer select-none px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 hover:text-gray-700 sm:px-4 dark:text-gray-400 dark:hover:text-gray-300"
    >
      {label}
      <SortIcon direction={isActive ? currentDirection : 'asc'} active={isActive} />
    </th>
  )
}

function StaticHeader({ label }: { label: string }) {
  return (
    <th className="hidden px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 sm:px-4 md:table-cell dark:text-gray-400">
      {label}
    </th>
  )
}

function SuiteRow({ suite, isInactive, hidden }: { suite: SuiteEntry; isInactive?: boolean; hidden?: boolean }) {
  const navigate = useNavigate()
  const { data: suiteInfo } = useSuite(suite.hash)

  if (hidden) return null

  return (
    <tr
      onClick={() => navigate({ to: '/suites/$suiteHash', params: { suiteHash: suite.hash } })}
      className={clsx(
        'cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50',
        isInactive && 'opacity-40',
      )}
    >
      <td className="whitespace-nowrap px-3 py-2 text-sm/6 text-gray-500 sm:px-4 sm:py-2.5 dark:text-gray-400">
        <span className="flex flex-col" title={formatRelativeTime(suite.lastRun)}>
          <span>{formatTimestampDate(suite.lastRun)}</span>
          <span className="text-xs/4 text-gray-400 dark:text-gray-500">{formatTimestampTime(suite.lastRun)}</span>
        </span>
      </td>
      <td className="px-3 py-2 sm:px-4 sm:py-2.5">
        <div className="flex items-center gap-2">
          <JDenticon value={suite.hash} size={24} className="shrink-0 self-start rounded-xs" />
          <div className="flex flex-col">
            <Link
              to="/suites/$suiteHash"
              params={{ suiteHash: suite.hash }}
              onClick={(e) => e.stopPropagation()}
              className="text-sm/6 font-medium text-blue-600 hover:text-blue-800 hover:underline dark:text-blue-400 dark:hover:text-blue-300"
            >
              {suiteInfo?.metadata?.labels?.name ?? <span className="font-mono">{suite.hash}</span>}
            </Link>
            {suiteInfo?.metadata?.labels?.name && (
              <span className="font-mono text-xs text-gray-500 dark:text-gray-400">{suite.hash}</span>
            )}
            {suiteInfo?.metadata?.labels && Object.keys(suiteInfo.metadata.labels).some((k) => k !== 'name') && (
              <div className="flex flex-wrap gap-1">
                {Object.entries(suiteInfo.metadata.labels)
                  .filter(([key]) => key !== 'name')
                  .map(([key, value]) => (
                    <span
                      key={key}
                      className="inline-flex items-center gap-1 rounded-xs bg-gray-100 px-1.5 py-0.5 text-xs/4 text-gray-600 dark:bg-gray-700 dark:text-gray-400"
                    >
                      <span className="font-medium text-gray-500 dark:text-gray-500">{key}:</span>
                      {value}
                    </span>
                  ))}
              </div>
            )}
          </div>
        </div>
      </td>
      <td className="hidden whitespace-nowrap px-3 py-2 sm:px-4 sm:py-2.5 md:table-cell">
        {suiteInfo?.source ? (
          <div className="flex items-center gap-2">
            <SourceBadge source={suiteInfo.source} />
            <Badge variant="default">{suiteInfo.tests?.length ?? 0}</Badge>
          </div>
        ) : (
          <span className="text-gray-400 dark:text-gray-500">-</span>
        )}
      </td>
      <td className="hidden whitespace-nowrap px-3 py-2 sm:px-4 sm:py-2.5 md:table-cell">
        {suiteInfo?.pre_run_steps && suiteInfo.pre_run_steps.length > 0 ? (
          <Badge variant="default">{suiteInfo.pre_run_steps.length}</Badge>
        ) : (
          <span className="text-gray-400 dark:text-gray-500">-</span>
        )}
      </td>
      <td className="hidden whitespace-nowrap px-3 py-2 sm:px-4 sm:py-2.5 md:table-cell">
        {suiteInfo?.filter ? (
          <span className="font-mono text-sm/6 text-gray-700 dark:text-gray-300">{suiteInfo.filter}</span>
        ) : (
          <span className="text-gray-400 dark:text-gray-500">-</span>
        )}
      </td>
      <td className="whitespace-nowrap px-3 py-2 text-right sm:px-4 sm:py-2.5">
        <Badge variant="info">{suite.runCount}</Badge>
      </td>
    </tr>
  )
}

const DEFAULT_INACTIVE_THRESHOLD_MS = 7 * 24 * 60 * 60 * 1000

function useNow() {
  const [now] = useState(() => Date.now())
  return now
}

export function SuitesTable({
  suites,
  sortBy = 'lastRun',
  sortDir = 'desc',
  onSortChange,
  hideInactive,
  inactiveThresholdMs = DEFAULT_INACTIVE_THRESHOLD_MS,
}: SuitesTableProps) {
  const now = useNow()
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
    <div className="overflow-x-auto rounded-xs bg-white shadow-xs dark:bg-gray-800">
      <table className="min-w-full table-fixed divide-y divide-gray-200 dark:divide-gray-700">
        <colgroup><col className="w-28" /><col /><col className="w-32" /><col className="w-28" /><col className="w-32" /><col className="w-24" /></colgroup>
        <thead className="bg-gray-50 dark:bg-gray-900">
          <tr>
            <SortableHeader label="Last Run" column="lastRun" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <SortableHeader label="Suite" column="hash" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <StaticHeader label="Source" />
            <StaticHeader label="Pre-Run Steps" />
            <StaticHeader label="Filter" />
            <SortableHeader label="Runs" column="runs" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
          {sortedSuites.map((suite) => (
            <SuiteRow
              key={suite.hash}
              suite={suite}
              isInactive={(now - suite.lastRun * 1000) > inactiveThresholdMs}
              hidden={hideInactive && (now - suite.lastRun * 1000) > inactiveThresholdMs}
            />
          ))}
        </tbody>
      </table>
    </div>
  )
}
