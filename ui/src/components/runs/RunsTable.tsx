import { Fragment, useState } from 'react'
import { Link } from '@tanstack/react-router'
import clsx from 'clsx'
import { type IndexEntry, type IndexStepType, ALL_INDEX_STEP_TYPES, getIndexAggregatedStats } from '@/api/types'
import { useSuite } from '@/api/hooks/useSuite'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { Badge } from '@/components/shared/Badge'
import { Duration } from '@/components/shared/Duration'
import { JDenticon } from '@/components/shared/JDenticon'
import { StrategyIcon } from '@/components/shared/StrategyIcon'
import { Tag } from 'lucide-react'
import { formatTimestampDate, formatTimestampTime, formatRelativeTime } from '@/utils/date'
import { formatDuration, formatNumber } from '@/utils/format'
import { type SortColumn, type SortDirection } from './sortEntries'

// Calculates MGas/s from gas_used and gas_used_duration
function calculateMGasPerSec(gasUsed: number, gasUsedDuration: number): number | undefined {
  if (gasUsedDuration <= 0 || gasUsed <= 0) return undefined
  return (gasUsed * 1000) / gasUsedDuration
}

interface RunsTableProps {
  entries: IndexEntry[]
  sortBy?: SortColumn
  sortDir?: SortDirection
  onSortChange?: (column: SortColumn, direction: SortDirection) => void
  showSuite?: boolean
  stepFilter?: IndexStepType[]
  selectable?: boolean
  selectedRunIds?: Set<string>
  onSelectionChange?: (runId: string, selected: boolean) => void
  selectionVariant?: 'compare' | 'delete'
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
  shortLabel,
  column,
  currentSort,
  currentDirection,
  onSort,
  className,
}: {
  label: string
  shortLabel?: string
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
      className={clsx('cursor-pointer select-none text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300', className ?? 'px-3 py-2 sm:px-4 sm:py-2')}
    >
      {shortLabel ? (
        <>
          <span className="sm:hidden">{shortLabel}</span>
          <span className="hidden sm:inline">{label}</span>
        </>
      ) : label}
      <SortIcon direction={isActive ? currentDirection : 'asc'} active={isActive} />
    </th>
  )
}

function SuiteCell({ suiteHash }: { suiteHash: string }) {
  const { data: suiteInfo } = useSuite(suiteHash)
  const name = suiteInfo?.metadata?.labels?.name
  const labels = suiteInfo?.metadata?.labels
    ? Object.entries(suiteInfo.metadata.labels).filter(([k]) => k !== 'name')
    : []

  return (
    <div className="group/suite relative flex items-center gap-2">
      <JDenticon value={suiteHash} size={20} className="shrink-0 rounded-xs" />
      <Link
        to="/suites/$suiteHash"
        params={{ suiteHash }}
        onClick={(e) => e.stopPropagation()}
        className="text-blue-600 hover:text-blue-800 hover:underline dark:text-blue-400 dark:hover:text-blue-300"
      >
        {suiteHash.slice(0, 4)}
      </Link>
      <div className="pointer-events-none absolute top-full left-0 z-50 mt-1 hidden w-max max-w-xs rounded-sm bg-white px-3 py-2 text-xs/5 shadow-lg ring-1 ring-gray-200 group-hover/suite:block dark:bg-gray-800 dark:ring-gray-700">
        <div className="flex flex-col gap-1.5">
          {name && <div className="font-medium text-gray-900 dark:text-gray-100">{name}</div>}
          <div className="font-mono text-gray-400 dark:text-gray-500">{suiteHash}</div>
          {suiteInfo?.filter && (
            <div className="text-gray-500 dark:text-gray-400">Filter: {suiteInfo.filter}</div>
          )}
          {suiteInfo && (
            <div className="text-gray-500 dark:text-gray-400">{suiteInfo.tests.length} tests</div>
          )}
          {labels.length > 0 && (
            <div className="flex flex-wrap gap-1">
              {labels.map(([k, v]) => (
                <span key={k} className="inline-flex items-center gap-1 rounded-xs border border-blue-200 bg-blue-50 px-1.5 py-0.5 text-xs/4 font-medium text-blue-700 dark:border-blue-800 dark:bg-blue-900/30 dark:text-blue-300">
                  <span className="font-semibold">{k}</span>={v}
                </span>
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

export function RunsTable({
  entries,
  sortBy = 'timestamp',
  sortDir = 'desc',
  onSortChange,
  showSuite = false,
  stepFilter = ALL_INDEX_STEP_TYPES,
  selectable = false,
  selectedRunIds,
  onSelectionChange,
  selectionVariant = 'compare',
}: RunsTableProps) {
  const [expandedRows, setExpandedRows] = useState<Set<string>>(new Set())

  const toggleExpanded = (runId: string) => {
    setExpandedRows((prev) => {
      const next = new Set(prev)
      if (next.has(runId)) next.delete(runId)
      else next.add(runId)
      return next
    })
  }

  const handleSort = (column: SortColumn) => {
    if (onSortChange) {
      const newDirection = sortBy === column && sortDir === 'desc' ? 'asc' : 'desc'
      onSortChange(column, column === sortBy ? newDirection : 'desc')
    }
  }

  return (
    <div className="overflow-x-auto rounded-xs bg-white shadow-xs dark:bg-gray-800">
      <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
        <thead className="bg-gray-50 dark:bg-gray-900">
          <tr>
            {selectable && <th className="w-10 px-2 py-2 sm:px-3 sm:py-2" />}
            <SortableHeader label="Time" column="timestamp" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <SortableHeader label="Client" column="client" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <SortableHeader label="Image" column="image" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="hidden px-3 py-2 sm:table-cell sm:px-4 sm:py-2" />
            {showSuite && <SortableHeader label="Suite" column="suite" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />}
            <SortableHeader label="MGas/s" column="mgas" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <SortableHeader label="Duration" column="duration" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
            <SortableHeader label="F" column="failed" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="px-1.5 py-2 sm:px-2 sm:py-2" />
            <SortableHeader label="P" column="passed" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="px-1.5 py-2 sm:px-2 sm:py-2" />
            <SortableHeader label="T" column="total" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="px-1.5 py-2 sm:px-2 sm:py-2" />
            <th className="w-8 px-1 py-2" />
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
          {entries.map((entry) => {
            const hasFailures = entry.status !== 'container_died' && entry.status !== 'cancelled' && entry.status !== 'timeout' && entry.tests.tests_total - entry.tests.tests_passed > 0
            const entryLabels = entry.metadata
              ? Object.entries(entry.metadata).filter(([k]) => !k.startsWith('github.') && k !== 'name')
              : []
            const colSpan = (selectable ? 1 : 0) + 3 + (showSuite ? 1 : 0) + 6
            return (
            <Fragment key={entry.run_id}>
            <tr
              onClick={selectable ? () => onSelectionChange?.(entry.run_id, !selectedRunIds?.has(entry.run_id)) : undefined}
              className={clsx(
                'group relative cursor-pointer transition-colors hover:z-20 hover:bg-gray-50 dark:hover:bg-gray-700/50',
                entry.status === 'container_died' && 'bg-red-50/50 dark:bg-red-900/10',
                entry.status === 'cancelled' && 'bg-yellow-50/50 dark:bg-yellow-900/10',
                entry.status === 'timeout' && 'bg-orange-50/50 dark:bg-orange-900/10',
                hasFailures && 'bg-orange-50/50 dark:bg-orange-900/10',
                selectable && selectedRunIds?.has(entry.run_id) && selectionVariant === 'compare' && 'ring-2 ring-inset ring-blue-400 dark:ring-blue-500',
                selectable && selectedRunIds?.has(entry.run_id) && selectionVariant === 'delete' && 'ring-2 ring-inset ring-red-400 dark:ring-red-500',
              )}
            >
              {selectable && (
                <td className="relative z-10 whitespace-nowrap px-2 py-2 text-center sm:px-3 sm:py-4">
                  <input
                    type="checkbox"
                    checked={selectedRunIds?.has(entry.run_id) ?? false}
                    onChange={(e) => {
                      e.stopPropagation()
                      onSelectionChange?.(entry.run_id, e.target.checked)
                    }}
                    onClick={(e) => e.stopPropagation()}
                    className={clsx('size-4 rounded-xs border-gray-300 dark:border-gray-600', selectionVariant === 'delete' ? 'text-red-600 focus:ring-red-500' : 'text-blue-600 focus:ring-blue-500')}
                  />
                </td>
              )}
              <td className={clsx(
                'whitespace-nowrap px-3 py-2 text-sm/6 text-gray-500 sm:px-4 sm:py-2.5 dark:text-gray-400 border-l-3',
                entry.status === 'container_died' && 'border-red-400 dark:border-red-500',
                entry.status === 'cancelled' && 'border-yellow-400 dark:border-yellow-500',
                entry.status === 'timeout' && 'border-orange-400 dark:border-orange-500',
                hasFailures && 'border-orange-400 dark:border-orange-500',
                entry.status !== 'container_died' && entry.status !== 'cancelled' && entry.status !== 'timeout' && !hasFailures && 'border-transparent',
              )}>
                {!selectable && (
                  <Link
                    to="/runs/$runId"
                    params={{ runId: entry.run_id }}
                    className="absolute inset-0 z-0"
                    tabIndex={-1}
                    aria-hidden
                  />
                )}
                <span className="flex flex-col" title={formatRelativeTime(entry.timestamp)}>
                  <span>{formatTimestampDate(entry.timestamp)}</span>
                  <span className="text-xs/4 text-gray-400 dark:text-gray-500">{formatTimestampTime(entry.timestamp)}</span>
                </span>
              </td>
              <td className="whitespace-nowrap px-3 py-2 sm:px-4 sm:py-2.5">
                <div className="flex items-center gap-2">
                  <span className="sm:hidden">
                    <ClientBadge client={entry.instance.client} hideLabel />
                  </span>
                  <span className="hidden sm:inline-flex">
                    <ClientBadge client={entry.instance.client} />
                  </span>
                  <StrategyIcon strategy={entry.instance.rollback_strategy} />
                </div>
              </td>
              <td className="hidden max-w-xs truncate px-3 py-2 font-mono text-sm/6 text-gray-500 sm:table-cell sm:px-4 sm:py-2.5 dark:text-gray-400">
                <span title={entry.instance.image}>{entry.instance.image}</span>
              </td>
              {showSuite && (
                <td className="relative z-10 whitespace-nowrap px-3 py-2 font-mono text-sm/6 sm:px-4 sm:py-2.5">
                  {entry.suite_hash ? (
                    <SuiteCell suiteHash={entry.suite_hash} />
                  ) : (
                    <span className="text-gray-400 dark:text-gray-500">-</span>
                  )}
                </td>
              )}
              {(() => {
                const stats = getIndexAggregatedStats(entry, stepFilter)
                return (
                  <>
                    <td
                      className="whitespace-nowrap px-3 py-2 text-right text-sm/6 text-gray-500 sm:px-4 sm:py-2.5 dark:text-gray-400"
                      title={(() => {
                        const testStep = entry.tests.steps.test
                        if (!testStep) return undefined
                        const parts = []
                        parts.push(`Duration: ${formatDuration(testStep.gas_used_duration)}`)
                        parts.push(`Gas used: ${formatNumber(testStep.gas_used)}`)
                        return parts.join('\n')
                      })()}
                    >
                      {(() => {
                        const mgas = calculateMGasPerSec(stats.gasUsed, stats.gasUsedDuration)
                        return mgas !== undefined ? mgas.toFixed(2) : '-'
                      })()}
                    </td>
                    <td className="whitespace-nowrap px-3 py-2 text-right text-sm/6 text-gray-500 sm:px-4 sm:py-2.5 dark:text-gray-400">
                      {entry.timestamp_end
                        ? <Duration nanoseconds={(entry.timestamp_end - entry.timestamp) * 1_000_000_000} />
                        : '-'}
                    </td>
                    <td className="whitespace-nowrap px-1.5 py-2 text-center sm:px-2 sm:py-2.5">
                      {entry.tests.tests_total - entry.tests.tests_passed > 0 && (
                        <Badge variant="error">{entry.tests.tests_total - entry.tests.tests_passed}</Badge>
                      )}
                    </td>
                    <td className="whitespace-nowrap px-1.5 py-2 text-center sm:px-2 sm:py-2.5">
                      <Badge variant="success">{entry.tests.tests_passed}</Badge>
                    </td>
                    <td className="whitespace-nowrap px-1.5 py-2 text-center sm:px-2 sm:py-2.5">
                      <Badge>{entry.tests.tests_total}</Badge>
                    </td>
                  </>
                )
              })()}
              <td className="relative z-10 px-1 py-2 text-center">
                {entryLabels.length > 0 && (
                  <div className="group/tag relative inline-block">
                    <button
                      onClick={(e) => { e.stopPropagation(); e.preventDefault(); toggleExpanded(entry.run_id) }}
                      className={clsx(
                        'rounded-xs p-0.5 transition-colors',
                        expandedRows.has(entry.run_id)
                          ? 'text-blue-600 dark:text-blue-400'
                          : 'text-gray-400 hover:text-gray-600 dark:text-gray-500 dark:hover:text-gray-300',
                      )}
                    >
                      <Tag className="size-3.5" />
                    </button>
                    <div className="pointer-events-none absolute right-0 top-full z-50 mt-1 hidden w-max max-w-xs rounded-sm bg-white px-3 py-2 text-xs/5 shadow-lg ring-1 ring-gray-200 group-hover/tag:block dark:bg-gray-800 dark:ring-gray-700">
                      <div className="flex flex-col gap-1.5">
                        <div className="text-gray-400 dark:text-gray-500">Instance ID: {entry.instance.id}</div>
                        <div className="flex flex-wrap gap-1">
                          {entryLabels.map(([k, v]) => (
                            <span key={k} className="inline-flex items-center gap-1 rounded-xs border border-blue-200 bg-blue-50 px-1.5 py-0.5 text-xs/4 font-medium text-blue-700 dark:border-blue-800 dark:bg-blue-900/30 dark:text-blue-300">
                              <span className="font-semibold">{k}</span>={v}
                            </span>
                          ))}
                        </div>
                      </div>
                    </div>
                  </div>
                )}
              </td>
            </tr>
            {entryLabels.length > 0 && expandedRows.has(entry.run_id) && (
              <tr>
                <td colSpan={colSpan} className="bg-gray-50/50 px-3 py-1.5 sm:px-4 dark:bg-gray-900/30">
                  <div className="flex flex-wrap items-center justify-end gap-1.5">
                    <span className="text-xs/4 text-gray-400 dark:text-gray-500">
                      ID: {entry.instance.id}
                    </span>
                    {entryLabels.map(([k, v]) => (
                      <span key={k} className="inline-flex items-center gap-1 rounded-xs border border-blue-200 bg-blue-50 px-1.5 py-0.5 text-xs/4 font-medium text-blue-700 dark:border-blue-800 dark:bg-blue-900/30 dark:text-blue-300">
                        <span className="font-semibold">{k}</span>
                        <span>=</span>
                        <span>{v}</span>
                      </span>
                    ))}
                  </div>
                </td>
              </tr>
            )}
            </Fragment>
          )})}
        </tbody>
      </table>
    </div>
  )
}
