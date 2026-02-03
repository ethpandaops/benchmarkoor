import clsx from 'clsx'
import type { ProcessedTestData, DashboardState, SortField, SortOrder } from '../types'
import { CATEGORY_COLORS } from '../utils/colors'

interface BlockLogsTableProps {
  data: ProcessedTestData[]
  state: DashboardState
  onUpdate: (updates: Partial<DashboardState>) => void
  onToggleSelection: (testName: string) => void
}

function formatMs(ms: number): string {
  if (ms < 1) return `${(ms * 1000).toFixed(0)}µs`
  if (ms < 1000) return `${ms.toFixed(1)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

function formatPercent(value: number): string {
  return `${value.toFixed(1)}%`
}

interface SortHeaderProps {
  label: string
  field: SortField
  currentSort: SortField
  currentOrder: SortOrder
  onSort: (field: SortField, order: SortOrder) => void
}

function SortHeader({ label, field, currentSort, currentOrder, onSort }: SortHeaderProps) {
  const isActive = currentSort === field
  const nextOrder: SortOrder = isActive && currentOrder === 'desc' ? 'asc' : 'desc'

  return (
    <button
      onClick={() => onSort(field, nextOrder)}
      className={clsx(
        'flex items-center gap-1 text-left font-medium',
        isActive ? 'text-blue-600 dark:text-blue-400' : 'text-gray-700 dark:text-gray-300'
      )}
    >
      {label}
      {isActive && (
        <svg className="size-3" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          {currentOrder === 'asc' ? (
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
          ) : (
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
          )}
        </svg>
      )}
    </button>
  )
}

export function BlockLogsTable({ data, state, onUpdate, onToggleSelection }: BlockLogsTableProps) {
  const handleSort = (field: SortField, order: SortOrder) => {
    onUpdate({ sortBy: field, sortOrder: order })
  }

  if (data.length === 0) {
    return (
      <div className="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
        No tests match the current filters.
      </div>
    )
  }

  return (
    <div className="overflow-x-auto border-t border-gray-200 dark:border-gray-700">
      <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
        <thead className="bg-gray-50 dark:bg-gray-800/50">
          <tr>
            <th scope="col" className="w-10 px-3 py-3">
              <span className="sr-only">Select</span>
            </th>
            <th scope="col" className="sticky left-0 z-10 bg-gray-50 px-3 py-3 text-left text-xs dark:bg-gray-800/50">
              <SortHeader
                label="Test Name"
                field="name"
                currentSort={state.sortBy}
                currentOrder={state.sortOrder}
                onSort={handleSort}
              />
            </th>
            <th scope="col" className="px-3 py-3 text-left text-xs">
              <span className="font-medium text-gray-700 dark:text-gray-300">Category</span>
            </th>
            <th scope="col" className="px-3 py-3 text-right text-xs">
              <SortHeader
                label="MGas/s"
                field="throughput"
                currentSort={state.sortBy}
                currentOrder={state.sortOrder}
                onSort={handleSort}
              />
            </th>
            <th scope="col" className="px-3 py-3 text-right text-xs">
              <SortHeader
                label="Execution"
                field="execution"
                currentSort={state.sortBy}
                currentOrder={state.sortOrder}
                onSort={handleSort}
              />
            </th>
            <th scope="col" className="px-3 py-3 text-right text-xs" title="state_read + state_hash + commit">
              <SortHeader
                label="Overhead"
                field="overhead"
                currentSort={state.sortBy}
                currentOrder={state.sortOrder}
                onSort={handleSort}
              />
            </th>
            <th scope="col" className="px-3 py-3 text-right text-xs">
              <span className="font-medium text-gray-700 dark:text-gray-300">Acct Cache</span>
            </th>
            <th scope="col" className="px-3 py-3 text-right text-xs">
              <span className="font-medium text-gray-700 dark:text-gray-300">Storage Cache</span>
            </th>
            <th scope="col" className="px-3 py-3 text-right text-xs">
              <span className="font-medium text-gray-700 dark:text-gray-300">Code Cache</span>
            </th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
          {data.map((row) => {
            const isSelected = state.selectedTests.includes(row.testName)
            const canSelect = state.selectedTests.length < 5 || isSelected

            return (
              <tr
                key={row.testName}
                onClick={() => canSelect && onToggleSelection(row.testName)}
                className={clsx(
                  'cursor-pointer transition-colors',
                  isSelected
                    ? 'bg-blue-50 dark:bg-blue-900/20'
                    : 'hover:bg-gray-50 dark:hover:bg-gray-800/50',
                  !canSelect && !isSelected && 'cursor-not-allowed opacity-50'
                )}
              >
                <td className="px-3 py-2">
                  <input
                    type="checkbox"
                    checked={isSelected}
                    onChange={() => canSelect && onToggleSelection(row.testName)}
                    disabled={!canSelect}
                    className="rounded-xs border-gray-300 text-blue-600 focus:ring-blue-500 disabled:opacity-50 dark:border-gray-600"
                  />
                </td>
                <td className="sticky left-0 z-10 max-w-xs truncate bg-white px-3 py-2 text-sm text-gray-900 dark:bg-gray-800 dark:text-gray-100">
                  <span title={row.testName}>{row.testName}</span>
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
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
