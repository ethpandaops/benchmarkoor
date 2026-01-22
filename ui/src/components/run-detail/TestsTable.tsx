import { useMemo } from 'react'
import clsx from 'clsx'
import type { TestEntry, SuiteTest, AggregatedStats } from '@/api/types'
import { Badge } from '@/components/shared/Badge'
import { Duration } from '@/components/shared/Duration'
import { Pagination } from '@/components/shared/Pagination'

export type TestSortColumn = 'order' | 'name' | 'time' | 'mgas' | 'passed' | 'failed'
export type TestSortDirection = 'asc' | 'desc'
export type TestStatusFilter = 'all' | 'passed' | 'failed'

const PAGE_SIZE_OPTIONS = [20, 50, 100] as const
const DEFAULT_PAGE_SIZE = 20

// Aggregate stats from all steps of a test entry
function getAggregatedStats(entry: TestEntry): AggregatedStats | undefined {
  if (!entry.steps) return undefined

  const steps = [entry.steps.setup, entry.steps.test, entry.steps.cleanup].filter((s) => s?.aggregated)

  if (steps.length === 0) return undefined

  // Sum up stats from all steps
  let timeTotal = 0
  let gasUsedTotal = 0
  let gasUsedTimeTotal = 0
  let success = 0
  let fail = 0

  for (const step of steps) {
    if (step?.aggregated) {
      timeTotal += step.aggregated.time_total
      gasUsedTotal += step.aggregated.gas_used_total
      gasUsedTimeTotal += step.aggregated.gas_used_time_total
      success += step.aggregated.success
      fail += step.aggregated.fail
    }
  }

  return {
    time_total: timeTotal,
    gas_used_total: gasUsedTotal,
    gas_used_time_total: gasUsedTimeTotal,
    success,
    fail,
    msg_count: 0,
    method_stats: { times: {}, mgas_s: {} },
  }
}

interface TestsTableProps {
  tests: Record<string, TestEntry>
  suiteTests?: SuiteTest[]
  currentPage?: number
  pageSize?: number
  sortBy?: TestSortColumn
  sortDir?: TestSortDirection
  searchQuery?: string
  statusFilter?: TestStatusFilter
  onPageChange?: (page: number) => void
  onPageSizeChange?: (size: number) => void
  onSortChange?: (column: TestSortColumn, direction: TestSortDirection) => void
  onSearchChange?: (query: string) => void
  onStatusFilterChange?: (status: TestStatusFilter) => void
  onTestClick?: (testName: string) => void
}

function SortIcon({ direction, active }: { direction: TestSortDirection; active: boolean }) {
  return (
    <svg
      className={clsx('ml-1 inline-block size-3', active ? 'text-gray-700 dark:text-gray-300' : 'text-gray-400')}
      viewBox="0 0 12 12"
      fill="currentColor"
    >
      {direction === 'asc' ? <path d="M6 2L10 8H2L6 2Z" /> : <path d="M6 10L2 4H10L6 10Z" />}
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
  column: TestSortColumn
  currentSort: TestSortColumn
  currentDirection: TestSortDirection
  onSort: (column: TestSortColumn) => void
  className?: string
}) {
  const isActive = currentSort === column
  return (
    <th
      onClick={() => onSort(column)}
      className={clsx(
        'cursor-pointer select-none px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300',
        className,
      )}
    >
      {label}
      <SortIcon direction={isActive ? currentDirection : 'asc'} active={isActive} />
    </th>
  )
}


// Calculates MGas/s from gas_used_total and gas_used_time_total
function calculateMGasPerSec(gasUsedTotal: number, gasUsedTimeTotal: number): number | undefined {
  if (gasUsedTimeTotal <= 0 || gasUsedTotal <= 0) return undefined
  return (gasUsedTotal * 1000) / gasUsedTimeTotal
}

export function TestsTable({
  tests,
  suiteTests,
  currentPage = 1,
  pageSize = DEFAULT_PAGE_SIZE,
  sortBy = 'order',
  sortDir = 'asc',
  searchQuery = '',
  statusFilter = 'all',
  onPageChange,
  onPageSizeChange,
  onSortChange,
  onSearchChange,
  onStatusFilterChange,
  onTestClick,
}: TestsTableProps) {
  const executionOrder = useMemo(() => {
    if (!suiteTests) return new Map<string, number>()
    return new Map(suiteTests.map((test, index) => [test.name, index + 1]))
  }, [suiteTests])

  const handleSort = (column: TestSortColumn) => {
    if (onSortChange) {
      const newDirection = sortBy === column && sortDir === 'asc' ? 'desc' : 'asc'
      onSortChange(column, column === sortBy ? newDirection : 'asc')
    }
  }

  const sortedTests = useMemo(() => {
    let filtered = Object.entries(tests)

    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      filtered = filtered.filter(([name]) => name.toLowerCase().includes(query))
    }

    if (statusFilter === 'passed') {
      filtered = filtered.filter(([, entry]) => {
        const stats = getAggregatedStats(entry)
        return stats ? stats.fail === 0 : false
      })
    } else if (statusFilter === 'failed') {
      filtered = filtered.filter(([, entry]) => {
        const stats = getAggregatedStats(entry)
        return stats ? stats.fail > 0 : false
      })
    }

    return filtered.sort(([a, entryA], [b, entryB]) => {
      let comparison = 0
      const statsA = getAggregatedStats(entryA)
      const statsB = getAggregatedStats(entryB)

      if (sortBy === 'order') {
        // a and b are test names
        const orderA = executionOrder.get(a) ?? Infinity
        const orderB = executionOrder.get(b) ?? Infinity
        comparison = orderA - orderB
      } else if (sortBy === 'time') {
        comparison = (statsA?.time_total ?? 0) - (statsB?.time_total ?? 0)
      } else if (sortBy === 'mgas') {
        const mgasA = statsA ? calculateMGasPerSec(statsA.gas_used_total, statsA.gas_used_time_total) ?? -Infinity : -Infinity
        const mgasB = statsB ? calculateMGasPerSec(statsB.gas_used_total, statsB.gas_used_time_total) ?? -Infinity : -Infinity
        comparison = mgasA - mgasB
      } else if (sortBy === 'passed') {
        comparison = (statsA?.success ?? 0) - (statsB?.success ?? 0)
      } else if (sortBy === 'failed') {
        comparison = (statsA?.fail ?? 0) - (statsB?.fail ?? 0)
      } else {
        comparison = a.localeCompare(b)
      }
      return sortDir === 'asc' ? comparison : -comparison
    })
  }, [tests, searchQuery, statusFilter, executionOrder, sortBy, sortDir])

  const totalPages = Math.ceil(sortedTests.length / pageSize)
  const paginatedTests = sortedTests.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  const handleSearchInput = (value: string) => {
    if (onSearchChange) {
      onSearchChange(value)
    }
  }

  const handlePageSizeChange = (newSize: number) => {
    if (onPageSizeChange) {
      onPageSizeChange(newSize)
    }
  }

  const paginationControls = (
    <div className="flex items-center justify-between">
      <div className="flex items-center gap-2">
        <span className="text-sm/6 text-gray-500 dark:text-gray-400">Show</span>
        <select
          value={pageSize}
          onChange={(e) => handlePageSizeChange(Number(e.target.value))}
          className="rounded-sm border border-gray-300 bg-white px-2 py-1 text-sm/6 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
        >
          {PAGE_SIZE_OPTIONS.map((size) => (
            <option key={size} value={size}>
              {size}
            </option>
          ))}
        </select>
        <span className="text-sm/6 text-gray-500 dark:text-gray-400">per page</span>
      </div>
      {totalPages > 1 && <Pagination currentPage={currentPage} totalPages={totalPages} onPageChange={onPageChange ?? (() => {})} />}
    </div>
  )

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <h2 className="text-lg/7 font-semibold text-gray-900 dark:text-gray-100">Tests ({sortedTests.length})</h2>
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-1 rounded-sm bg-gray-100 p-0.5 dark:bg-gray-700">
            {(['all', 'passed', 'failed'] as const).map((status) => (
              <button
                key={status}
                onClick={() => onStatusFilterChange?.(status)}
                className={clsx(
                  'rounded-xs px-2 py-1 text-xs/5 font-medium capitalize transition-colors',
                  statusFilter === status
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                    : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                )}
              >
                {status}
              </button>
            ))}
          </div>
          <input
            type="text"
            placeholder="Search tests..."
            value={searchQuery}
            onChange={(e) => handleSearchInput(e.target.value)}
            className="w-64 rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm/6 placeholder:text-gray-400 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 dark:placeholder:text-gray-500"
          />
        </div>
      </div>

      {paginationControls}

      <div className="overflow-x-auto rounded-sm bg-white shadow-xs dark:bg-gray-800">
        <table className="min-w-full table-fixed divide-y divide-gray-200 dark:divide-gray-700">
          <thead className="bg-gray-50 dark:bg-gray-900">
            <tr>
              <SortableHeader
                label="#"
                column="order"
                currentSort={sortBy}
                currentDirection={sortDir}
                onSort={handleSort}
                className="w-12"
              />
              <SortableHeader label="Test" column="name" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
              <SortableHeader label="Total Time" column="time" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="w-28 text-right" />
              <SortableHeader label="MGas/s" column="mgas" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="w-24 text-right" />
              <SortableHeader label="Failed" column="failed" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="w-16 text-center" />
              <SortableHeader label="Passed" column="passed" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="w-16 text-center" />
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {paginatedTests.map(([testName, entry]) => {
              const stats = getAggregatedStats(entry)

              return (
                <tr
                  key={testName}
                  onClick={() => onTestClick?.(testName)}
                  className="cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50"
                >
                  <td className="whitespace-nowrap px-4 py-3 text-sm/6 font-medium text-gray-500 dark:text-gray-400">
                    {executionOrder.get(testName) ?? '-'}
                  </td>
                  <td className="max-w-md px-4 py-3">
                    <div className="truncate text-sm/6 font-medium text-gray-900 dark:text-gray-100" title={testName}>
                      {testName}
                    </div>
                    {entry.dir && (
                      <div className="truncate text-xs/5 text-gray-500 dark:text-gray-400" title={entry.dir}>
                        {entry.dir}
                      </div>
                    )}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-right text-sm/6 text-gray-500 dark:text-gray-400">
                    {stats ? <Duration nanoseconds={stats.time_total} /> : '-'}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-right text-sm/6 text-gray-500 dark:text-gray-400">
                    {(() => {
                      if (!stats) return '-'
                      const mgas = calculateMGasPerSec(stats.gas_used_total, stats.gas_used_time_total)
                      return mgas !== undefined ? mgas.toFixed(2) : '-'
                    })()}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-center">
                    {stats && stats.fail > 0 && <Badge variant="error">{stats.fail}</Badge>}
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-center">
                    {stats && stats.success > 0 && <Badge variant="success">{stats.success}</Badge>}
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      {paginationControls}
    </div>
  )
}
