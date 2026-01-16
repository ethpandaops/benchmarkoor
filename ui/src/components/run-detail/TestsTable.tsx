import { useMemo } from 'react'
import clsx from 'clsx'
import type { TestEntry, SuiteFile } from '@/api/types'
import { Badge } from '@/components/shared/Badge'
import { Duration } from '@/components/shared/Duration'
import { Pagination } from '@/components/shared/Pagination'
import { TimeBreakdown } from './TimeBreakdown'
import { MGasBreakdown } from './MGasBreakdown'
import { ExecutionsList } from './ExecutionsList'

export type TestSortColumn = 'order' | 'name' | 'time' | 'mgas'
export type TestSortDirection = 'asc' | 'desc'

const PAGE_SIZE_OPTIONS = [20, 50, 100] as const
const DEFAULT_PAGE_SIZE = 20

interface TestsTableProps {
  tests: Record<string, TestEntry>
  runId: string
  suiteHash?: string
  suiteTests?: SuiteFile[]
  currentPage?: number
  pageSize?: number
  sortBy?: TestSortColumn
  sortDir?: TestSortDirection
  searchQuery?: string
  expandedTest?: string
  onPageChange?: (page: number) => void
  onPageSizeChange?: (size: number) => void
  onSortChange?: (column: TestSortColumn, direction: TestSortDirection) => void
  onSearchChange?: (query: string) => void
  onExpandedChange?: (testName: string | undefined) => void
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

// Creates a unique key for a test by combining directory and filename.
function makeTestKey(filename: string, dir?: string): string {
  return dir ? `${dir}/${filename}` : filename
}

// Calculates MGas/s from gas_used_total and gas_used_time_total
function calculateMGasPerSec(gasUsedTotal: number, gasUsedTimeTotal: number): number | undefined {
  if (gasUsedTimeTotal <= 0 || gasUsedTotal <= 0) return undefined
  return (gasUsedTotal * 1000) / gasUsedTimeTotal
}

export function TestsTable({
  tests,
  runId,
  suiteHash,
  suiteTests,
  currentPage = 1,
  pageSize = DEFAULT_PAGE_SIZE,
  sortBy = 'order',
  sortDir = 'asc',
  searchQuery = '',
  expandedTest,
  onPageChange,
  onPageSizeChange,
  onSortChange,
  onSearchChange,
  onExpandedChange,
}: TestsTableProps) {
  const executionOrder = useMemo(() => {
    if (!suiteTests) return new Map<string, number>()
    return new Map(suiteTests.map((file, index) => [makeTestKey(file.f, file.d), index + 1]))
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

    return filtered.sort(([a, entryA], [b, entryB]) => {
      let comparison = 0
      if (sortBy === 'order') {
        // a and b are already full paths (dir/filename or filename)
        const orderA = executionOrder.get(a) ?? Infinity
        const orderB = executionOrder.get(b) ?? Infinity
        comparison = orderA - orderB
      } else if (sortBy === 'time') {
        comparison = entryA.aggregated.time_total - entryB.aggregated.time_total
      } else if (sortBy === 'mgas') {
        const mgasA = calculateMGasPerSec(entryA.aggregated.gas_used_total, entryA.aggregated.gas_used_time_total) ?? -Infinity
        const mgasB = calculateMGasPerSec(entryB.aggregated.gas_used_total, entryB.aggregated.gas_used_time_total) ?? -Infinity
        comparison = mgasA - mgasB
      } else {
        comparison = a.localeCompare(b)
      }
      return sortDir === 'asc' ? comparison : -comparison
    })
  }, [tests, searchQuery, executionOrder, sortBy, sortDir])

  const totalPages = Math.ceil(sortedTests.length / pageSize)
  const paginatedTests = sortedTests.slice((currentPage - 1) * pageSize, currentPage * pageSize)

  const toggleExpand = (testName: string) => {
    if (onExpandedChange) {
      onExpandedChange(expandedTest === testName ? undefined : testName)
    }
  }

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
      <div className="flex items-center justify-between">
        <h2 className="text-lg/7 font-semibold text-gray-900 dark:text-gray-100">Tests ({sortedTests.length})</h2>
        <input
          type="text"
          placeholder="Search tests..."
          value={searchQuery}
          onChange={(e) => handleSearchInput(e.target.value)}
          className="w-64 rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm/6 placeholder:text-gray-400 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 dark:placeholder:text-gray-500"
        />
      </div>

      {paginationControls}

      <div className="overflow-x-auto rounded-sm bg-white shadow-xs dark:bg-gray-800">
        <table className="min-w-full table-fixed divide-y divide-gray-200 dark:divide-gray-700">
          <thead className="bg-gray-50 dark:bg-gray-900">
            <tr>
              <th className="w-8 px-4 py-3"></th>
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
              <th className="w-24 px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Status
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {paginatedTests.map(([testKey, entry]) => {
              // testKey is now dir/filename or just filename
              // Extract just the filename for display
              const filename = entry.dir ? testKey.slice(entry.dir.length + 1) : testKey

              return (
                <>
                  <tr
                    key={testKey}
                    onClick={() => toggleExpand(testKey)}
                    className={clsx(
                      'cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50',
                      expandedTest === testKey && 'bg-blue-50 dark:bg-blue-900/20',
                    )}
                  >
                    <td className="px-4 py-3">
                      <svg
                        className={clsx('size-4 text-gray-400 transition-transform', expandedTest === testKey && 'rotate-90')}
                        fill="none"
                        viewBox="0 0 24 24"
                        stroke="currentColor"
                      >
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                      </svg>
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-sm/6 font-medium text-gray-500 dark:text-gray-400">
                      {executionOrder.get(testKey) ?? '-'}
                    </td>
                    <td className="max-w-md px-4 py-3">
                      <div className="truncate text-sm/6 font-medium text-gray-900 dark:text-gray-100" title={filename}>
                        {filename}
                      </div>
                      {entry.dir && (
                        <div className="truncate text-xs/5 text-gray-500 dark:text-gray-400" title={entry.dir}>
                          {entry.dir}
                        </div>
                      )}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right text-sm/6 text-gray-500 dark:text-gray-400">
                      <Duration nanoseconds={entry.aggregated.time_total} />
                    </td>
                    <td className="whitespace-nowrap px-4 py-3 text-right text-sm/6 text-gray-500 dark:text-gray-400">
                      {(() => {
                        const mgas = calculateMGasPerSec(entry.aggregated.gas_used_total, entry.aggregated.gas_used_time_total)
                        return mgas !== undefined ? mgas.toFixed(2) : '-'
                      })()}
                    </td>
                    <td className="whitespace-nowrap px-4 py-3">
                      <div className="flex items-center gap-2">
                        {entry.aggregated.success > 0 && <Badge variant="success">{entry.aggregated.success}</Badge>}
                        {entry.aggregated.fail > 0 && <Badge variant="error">{entry.aggregated.fail}</Badge>}
                      </div>
                    </td>
                  </tr>
                  {expandedTest === testKey && (
                    <tr key={`${testKey}-expanded`}>
                      <td colSpan={6} className="overflow-hidden bg-gray-50 px-4 py-4 dark:bg-gray-900/50">
                        <div className="flex flex-col gap-6 overflow-x-auto">
                          <TimeBreakdown methods={entry.aggregated.method_stats.times} />
                          <MGasBreakdown methods={entry.aggregated.method_stats.mgas_s} />
                          {suiteHash && (
                            <ExecutionsList runId={runId} suiteHash={suiteHash} testName={filename} dir={entry.dir} />
                          )}
                        </div>
                      </td>
                    </tr>
                  )}
                </>
              )
            })}
          </tbody>
        </table>
      </div>

      {paginationControls}
    </div>
  )
}
