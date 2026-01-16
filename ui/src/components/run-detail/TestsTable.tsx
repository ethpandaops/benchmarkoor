import { useMemo } from 'react'
import clsx from 'clsx'
import type { TestEntry, SuiteFile } from '@/api/types'
import { Badge } from '@/components/shared/Badge'
import { Duration } from '@/components/shared/Duration'
import { Pagination } from '@/components/shared/Pagination'
import { MethodBreakdown } from './MethodBreakdown'

export type TestSortColumn = 'order' | 'name'
export type TestSortDirection = 'asc' | 'desc'

const PAGE_SIZE_OPTIONS = [20, 50, 100] as const
const DEFAULT_PAGE_SIZE = 20

interface TestsTableProps {
  tests: Record<string, TestEntry>
  runId: string
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

export function TestsTable({
  tests,
  runId,
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
    return new Map(suiteTests.map((file, index) => [file.f, index + 1]))
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

    return filtered.sort(([a], [b]) => {
      let comparison = 0
      if (sortBy === 'order') {
        const orderA = executionOrder.get(a) ?? Infinity
        const orderB = executionOrder.get(b) ?? Infinity
        comparison = orderA - orderB
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

      <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
        <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
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
              <th className="px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Status
              </th>
              <th className="px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Total Time
              </th>
              <th className="px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Methods
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {paginatedTests.map(([testName, entry]) => (
              <>
                <tr
                  key={testName}
                  onClick={() => toggleExpand(testName)}
                  className={clsx(
                    'cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50',
                    expandedTest === testName && 'bg-blue-50 dark:bg-blue-900/20',
                  )}
                >
                  <td className="px-4 py-3">
                    <svg
                      className={clsx('size-4 text-gray-400 transition-transform', expandedTest === testName && 'rotate-90')}
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                    >
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                    </svg>
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-sm/6 font-medium text-gray-500 dark:text-gray-400">
                    {executionOrder.get(testName) ?? '-'}
                  </td>
                  <td className="max-w-md truncate px-4 py-3 text-sm/6 font-medium text-gray-900 dark:text-gray-100">
                    <span title={testName}>{testName}</span>
                  </td>
                  <td className="whitespace-nowrap px-4 py-3">
                    <div className="flex items-center gap-2">
                      {entry.aggregated.success > 0 && <Badge variant="success">{entry.aggregated.success}</Badge>}
                      {entry.aggregated.fail > 0 && <Badge variant="error">{entry.aggregated.fail}</Badge>}
                    </div>
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-sm/6 text-gray-500 dark:text-gray-400">
                    <Duration nanoseconds={entry.aggregated.time_total} />
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-sm/6 text-gray-500 dark:text-gray-400">
                    {Object.keys(entry.aggregated.methods).length}
                  </td>
                </tr>
                {expandedTest === testName && (
                  <tr key={`${testName}-expanded`}>
                    <td colSpan={6} className="bg-gray-50 px-4 py-4 dark:bg-gray-900/50">
                      <MethodBreakdown methods={entry.aggregated.methods} runId={runId} testName={testName} dir={entry.dir} />
                    </td>
                  </tr>
                )}
              </>
            ))}
          </tbody>
        </table>
      </div>

      {paginationControls}
    </div>
  )
}
