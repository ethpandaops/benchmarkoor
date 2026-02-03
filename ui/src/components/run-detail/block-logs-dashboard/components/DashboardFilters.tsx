import { Listbox, ListboxButton, ListboxOption, ListboxOptions } from '@headlessui/react'
import clsx from 'clsx'
import type { DashboardState, DashboardStats, TestCategory, SortField } from '../types'
import { CATEGORY_COLORS } from '../utils/colors'

interface DashboardFiltersProps {
  state: DashboardState
  stats: DashboardStats | null
  onUpdate: (updates: Partial<DashboardState>) => void
}

const CATEGORY_OPTIONS: { value: 'all' | TestCategory; label: string }[] = [
  { value: 'all', label: 'All Categories' },
  { value: 'add', label: 'Add' },
  { value: 'mul', label: 'Multiply' },
  { value: 'pairing', label: 'Pairing' },
  { value: 'other', label: 'Other' },
]

const SORT_OPTIONS: { value: SortField; label: string; title?: string }[] = [
  { value: 'throughput', label: 'Throughput' },
  { value: 'execution', label: 'Execution Time' },
  { value: 'overhead', label: 'Overhead', title: 'state_read + state_hash + commit' },
  { value: 'name', label: 'Name' },
]

export function DashboardFilters({ state, stats, onUpdate }: DashboardFiltersProps) {
  const selectedCategory = CATEGORY_OPTIONS.find((o) => o.value === state.category) ?? CATEGORY_OPTIONS[0]
  const selectedSort = SORT_OPTIONS.find((o) => o.value === state.sortBy) ?? SORT_OPTIONS[0]

  return (
    <div className="flex flex-wrap items-center gap-4 border-b border-gray-200 px-4 py-3 dark:border-gray-700">
      {/* Category Filter */}
      <div className="flex items-center gap-2">
        <span className="text-xs text-gray-500 dark:text-gray-400">Category:</span>
        <Listbox value={state.category} onChange={(value) => onUpdate({ category: value })}>
          <div className="relative">
            <ListboxButton className="flex items-center gap-2 rounded-sm border border-gray-300 bg-white px-3 py-1.5 text-left text-sm dark:border-gray-600 dark:bg-gray-700">
              {state.category !== 'all' && (
                <span
                  className="size-2.5 rounded-full"
                  style={{ backgroundColor: CATEGORY_COLORS[state.category as TestCategory] }}
                />
              )}
              <span className="text-gray-900 dark:text-gray-100">{selectedCategory.label}</span>
              {stats?.categoryBreakdown && state.category !== 'all' && (
                <span className="text-gray-500 dark:text-gray-400">
                  ({stats.categoryBreakdown[state.category as TestCategory]})
                </span>
              )}
              <svg className="size-4 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
              </svg>
            </ListboxButton>
            <ListboxOptions className="absolute z-10 mt-1 max-h-60 w-48 overflow-auto rounded-sm border border-gray-200 bg-white py-1 shadow-lg dark:border-gray-600 dark:bg-gray-700">
              {CATEGORY_OPTIONS.map((option) => (
                <ListboxOption
                  key={option.value}
                  value={option.value}
                  className={({ active, selected }) =>
                    clsx(
                      'flex cursor-pointer items-center gap-2 px-3 py-2 text-sm',
                      active && 'bg-gray-100 dark:bg-gray-600',
                      selected && 'font-medium'
                    )
                  }
                >
                  {option.value !== 'all' && (
                    <span
                      className="size-2.5 rounded-full"
                      style={{ backgroundColor: CATEGORY_COLORS[option.value] }}
                    />
                  )}
                  <span className="text-gray-900 dark:text-gray-100">{option.label}</span>
                  {stats?.categoryBreakdown && option.value !== 'all' && (
                    <span className="ml-auto text-gray-500 dark:text-gray-400">
                      {stats.categoryBreakdown[option.value]}
                    </span>
                  )}
                </ListboxOption>
              ))}
            </ListboxOptions>
          </div>
        </Listbox>
      </div>

      {/* Sort Options */}
      <div className="flex items-center gap-2">
        <span className="text-xs text-gray-500 dark:text-gray-400">Sort:</span>
        <Listbox value={state.sortBy} onChange={(value) => onUpdate({ sortBy: value })}>
          <div className="relative">
            <ListboxButton className="flex items-center gap-2 rounded-sm border border-gray-300 bg-white px-3 py-1.5 text-left text-sm dark:border-gray-600 dark:bg-gray-700">
              <span className="text-gray-900 dark:text-gray-100">{selectedSort.label}</span>
              <svg className="size-4 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
              </svg>
            </ListboxButton>
            <ListboxOptions className="absolute z-10 mt-1 max-h-60 w-40 overflow-auto rounded-sm border border-gray-200 bg-white py-1 shadow-lg dark:border-gray-600 dark:bg-gray-700">
              {SORT_OPTIONS.map((option) => (
                <ListboxOption
                  key={option.value}
                  value={option.value}
                  title={option.title}
                  className={({ active, selected }) =>
                    clsx(
                      'cursor-pointer px-3 py-2 text-sm text-gray-900 dark:text-gray-100',
                      active && 'bg-gray-100 dark:bg-gray-600',
                      selected && 'font-medium'
                    )
                  }
                >
                  {option.label}
                </ListboxOption>
              ))}
            </ListboxOptions>
          </div>
        </Listbox>
        <button
          onClick={() => onUpdate({ sortOrder: state.sortOrder === 'asc' ? 'desc' : 'asc' })}
          className="rounded-sm border border-gray-300 bg-white p-1.5 text-gray-500 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600"
          title={state.sortOrder === 'asc' ? 'Ascending' : 'Descending'}
        >
          {state.sortOrder === 'asc' ? (
            <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 15l7-7 7 7" />
            </svg>
          ) : (
            <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
            </svg>
          )}
        </button>
      </div>

      {/* Throughput Range */}
      <div className="flex items-center gap-2">
        <span className="text-xs text-gray-500 dark:text-gray-400">MGas/s:</span>
        <input
          type="number"
          placeholder="Min"
          value={state.minThroughput ?? ''}
          onChange={(e) => onUpdate({ minThroughput: e.target.value ? Number(e.target.value) : undefined })}
          className="w-16 rounded-sm border border-gray-300 bg-white px-2 py-1 text-sm dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
        />
        <span className="text-gray-400">-</span>
        <input
          type="number"
          placeholder="Max"
          value={state.maxThroughput ?? ''}
          onChange={(e) => onUpdate({ maxThroughput: e.target.value ? Number(e.target.value) : undefined })}
          className="w-16 rounded-sm border border-gray-300 bg-white px-2 py-1 text-sm dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
        />
      </div>

      {/* Toggles */}
      <div className="flex items-center gap-4">
        <label className="flex cursor-pointer items-center gap-1.5 text-xs text-gray-600 dark:text-gray-400">
          <input
            type="checkbox"
            checked={state.excludeOutliers}
            onChange={(e) => onUpdate({ excludeOutliers: e.target.checked })}
            className="rounded-xs border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600"
          />
          Exclude outliers
        </label>
        <label className="flex cursor-pointer items-center gap-1.5 text-xs text-gray-600 dark:text-gray-400">
          <input
            type="checkbox"
            checked={state.useLogScale}
            onChange={(e) => onUpdate({ useLogScale: e.target.checked })}
            className="rounded-xs border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600"
          />
          Log scale
        </label>
      </div>

      {/* Stats Summary */}
      {stats && (
        <div className="ml-auto text-xs text-gray-500 dark:text-gray-400">
          {stats.count} tests | Avg: {stats.avgThroughput.toFixed(1)} MGas/s
        </div>
      )}
    </div>
  )
}
