import { useState, useMemo } from 'react'
import clsx from 'clsx'
import { ChevronUp, ChevronDown } from 'lucide-react'
import type { ProcessedTestData, DashboardState, SortField, SortOrder } from '../types'
import { CATEGORY_COLORS } from '../utils/colors'
import { Pagination } from '@/components/shared/Pagination'

interface BlockLogsTableProps {
  data: ProcessedTestData[]
  state: DashboardState
  onUpdate: (updates: Partial<DashboardState>) => void
  onTestClick?: (testName: string) => void
}

const PAGE_SIZE_OPTIONS = [20, 50, 100]

function formatMs(ms: number): string {
  if (ms < 1) return `${(ms * 1000).toFixed(0)}µs`
  if (ms < 1000) return `${ms.toFixed(1)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

function formatPercent(value: number): string {
  return `${value.toFixed(1)}%`
}

function formatGas(gas: number): string {
  if (gas >= 1_000_000_000) return `${(gas / 1_000_000_000).toFixed(1)}B`
  if (gas >= 1_000_000) return `${(gas / 1_000_000).toFixed(1)}M`
  if (gas >= 1_000) return `${(gas / 1_000).toFixed(1)}K`
  return gas.toString()
}

interface SortHeaderProps {
  label: string
  field: SortField
  currentSort: SortField
  currentOrder: SortOrder
  onSort: (field: SortField, order: SortOrder) => void
  align?: 'left' | 'right'
}

function SortHeader({ label, field, currentSort, currentOrder, onSort, align = 'left' }: SortHeaderProps) {
  const isActive = currentSort === field
  const nextOrder: SortOrder = isActive && currentOrder === 'desc' ? 'asc' : 'desc'

  return (
    <button
      onClick={() => onSort(field, nextOrder)}
      className={clsx(
        'flex items-center gap-1 font-medium',
        align === 'right' ? 'ml-auto' : '',
        isActive ? 'text-blue-600 dark:text-blue-400' : 'text-gray-700 dark:text-gray-300'
      )}
    >
      {label}
      {isActive && (currentOrder === 'asc' ? <ChevronUp className="size-3" /> : <ChevronDown className="size-3" />)}
    </button>
  )
}

export function BlockLogsTable({ data, state, onUpdate, onTestClick }: BlockLogsTableProps) {
  const [currentPage, setCurrentPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)

  const totalPages = Math.ceil(data.length / pageSize)

  const paginatedData = useMemo(() => {
    const startIndex = (currentPage - 1) * pageSize
    return data.slice(startIndex, startIndex + pageSize)
  }, [data, currentPage, pageSize])

  // Reset to page 1 when data changes and current page would be out of bounds
  const maxPage = Math.ceil(data.length / pageSize) || 1
  if (currentPage > maxPage) {
    setCurrentPage(1)
  }

  const handleSort = (field: SortField, order: SortOrder) => {
    onUpdate({ sortBy: field, sortOrder: order })
    setCurrentPage(1)
  }

  const handlePageSizeChange = (newSize: number) => {
    setPageSize(newSize)
    setCurrentPage(1)
  }

  if (data.length === 0) {
    return (
      <div className="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
        No tests match the current filters.
      </div>
    )
  }

  const startItem = (currentPage - 1) * pageSize + 1
  const endItem = Math.min(currentPage * pageSize, data.length)

  const paginationControls = (
    <div className="flex items-center justify-between bg-gray-50 px-4 py-3 dark:bg-gray-800/50">
      <div className="flex items-center gap-2 text-sm text-gray-600 dark:text-gray-400">
        <span>Show</span>
        <select
          value={pageSize}
          onChange={(e) => handlePageSizeChange(Number(e.target.value))}
          className="rounded-sm border border-gray-300 bg-white px-2 py-1 text-sm dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
        >
          {PAGE_SIZE_OPTIONS.map((size) => (
            <option key={size} value={size}>
              {size}
            </option>
          ))}
        </select>
        <span>per page</span>
      </div>

      <div className="text-sm text-gray-600 dark:text-gray-400">
        {startItem}-{endItem} of {data.length}
      </div>

      <Pagination
        currentPage={currentPage}
        totalPages={totalPages}
        onPageChange={setCurrentPage}
      />
    </div>
  )

  return (
    <div className="flex flex-col">
      {/* Top Pagination */}
      <div className="border-t border-gray-200 dark:border-gray-700">
        {paginationControls}
      </div>

      <div className="overflow-x-auto border-t border-gray-200 dark:border-gray-700">
        <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
          <thead className="bg-gray-50 dark:bg-gray-800/50">
            <tr>
              <th scope="col" className="sticky left-0 z-10 bg-gray-50 px-3 py-3 text-left text-xs dark:bg-gray-800/50">
                <SortHeader
                  label="Test #"
                  field="order"
                  currentSort={state.sortBy}
                  currentOrder={state.sortOrder}
                  onSort={handleSort}
                />
              </th>
              <th scope="col" className="px-3 py-3 text-left text-xs">
                <SortHeader
                  label="Category"
                  field="category"
                  currentSort={state.sortBy}
                  currentOrder={state.sortOrder}
                  onSort={handleSort}
                />
              </th>
              <th scope="col" className="px-3 py-3 text-right text-xs">
                <SortHeader
                  label="MGas/s"
                  field="throughput"
                  currentSort={state.sortBy}
                  currentOrder={state.sortOrder}
                  onSort={handleSort}
                  align="right"
                />
              </th>
              <th scope="col" className="px-3 py-3 text-right text-xs">
                <SortHeader
                  label="Gas"
                  field="gas"
                  currentSort={state.sortBy}
                  currentOrder={state.sortOrder}
                  onSort={handleSort}
                  align="right"
                />
              </th>
              <th scope="col" className="px-3 py-3 text-right text-xs">
                <SortHeader
                  label="Execution"
                  field="execution"
                  currentSort={state.sortBy}
                  currentOrder={state.sortOrder}
                  onSort={handleSort}
                  align="right"
                />
              </th>
              <th scope="col" className="px-3 py-3 text-right text-xs" title="state_read + state_hash + commit">
                <SortHeader
                  label="Overhead"
                  field="overhead"
                  currentSort={state.sortBy}
                  currentOrder={state.sortOrder}
                  onSort={handleSort}
                  align="right"
                />
              </th>
              <th scope="col" className="px-3 py-3 text-right text-xs">
                <SortHeader
                  label="Acct Cache"
                  field="accountCache"
                  currentSort={state.sortBy}
                  currentOrder={state.sortOrder}
                  onSort={handleSort}
                  align="right"
                />
              </th>
              <th scope="col" className="px-3 py-3 text-right text-xs">
                <SortHeader
                  label="Storage Cache"
                  field="storageCache"
                  currentSort={state.sortBy}
                  currentOrder={state.sortOrder}
                  onSort={handleSort}
                  align="right"
                />
              </th>
              <th scope="col" className="px-3 py-3 text-right text-xs">
                <SortHeader
                  label="Code Cache"
                  field="codeCache"
                  currentSort={state.sortBy}
                  currentOrder={state.sortOrder}
                  onSort={handleSort}
                  align="right"
                />
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {paginatedData.map((row) => (
              <tr
                key={row.testName}
                onClick={() => onTestClick?.(row.testName)}
                className={`hover:bg-gray-50 dark:hover:bg-gray-800/50 ${onTestClick ? 'cursor-pointer' : ''}`}
              >
                <td className="sticky left-0 z-10 bg-white px-3 py-2 text-sm text-gray-900 dark:bg-gray-800 dark:text-gray-100">
                    <span title={row.testName} className="cursor-help">
                      {row.testOrder === Infinity ? '-' : row.testOrder}
                    </span>
                  </td>
                  <td className="px-3 py-2">
                    <span
                      className="inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium capitalize"
                      style={{
                        backgroundColor: `${CATEGORY_COLORS[row.category]}20`,
                        color: CATEGORY_COLORS[row.category],
                      }}
                    >
                      {row.category}
                    </span>
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-sm text-gray-900 dark:text-gray-100">
                    {row.throughput.toFixed(1)}
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-sm text-gray-600 dark:text-gray-400">
                    {formatGas(row.gasUsed)}
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-sm text-gray-600 dark:text-gray-400">
                    {formatMs(row.executionMs)}
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-sm text-gray-600 dark:text-gray-400">
                    {formatMs(row.overheadMs)}
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-sm">
                    <span
                      className={clsx(
                        row.accountCacheHitRate >= 80 ? 'text-green-600 dark:text-green-400' : 'text-orange-600 dark:text-orange-400'
                      )}
                    >
                      {formatPercent(row.accountCacheHitRate)}
                    </span>
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-sm">
                    <span
                      className={clsx(
                        row.storageCacheHitRate >= 80 ? 'text-green-600 dark:text-green-400' : 'text-orange-600 dark:text-orange-400'
                      )}
                    >
                      {formatPercent(row.storageCacheHitRate)}
                    </span>
                  </td>
                  <td className="px-3 py-2 text-right font-mono text-sm">
                    <span
                      className={clsx(
                        row.codeCacheHitRate >= 80 ? 'text-green-600 dark:text-green-400' : 'text-orange-600 dark:text-orange-400'
                      )}
                    >
                      {formatPercent(row.codeCacheHitRate)}
                    </span>
                  </td>
                </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Bottom Pagination */}
      <div className="border-t border-gray-200 dark:border-gray-700">
        {paginationControls}
      </div>
    </div>
  )
}
