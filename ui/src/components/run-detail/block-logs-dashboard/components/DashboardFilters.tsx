import { useState, useRef, useEffect } from 'react'
import type { DashboardState, DashboardStats, TestCategory, SortField } from '../types'
import { ALL_CATEGORIES, CATEGORY_COLORS } from '../utils/colors'

interface DashboardFiltersProps {
  state: DashboardState
  stats: DashboardStats | null
  onUpdate: (updates: Partial<DashboardState>) => void
}

const CATEGORY_OPTIONS: { value: TestCategory; label: string }[] = [
  // EVM Instructions
  { value: 'arithmetic', label: 'Arithmetic' },
  { value: 'memory', label: 'Memory' },
  { value: 'storage', label: 'Storage' },
  { value: 'stack', label: 'Stack' },
  { value: 'control', label: 'Control' },
  { value: 'keccak', label: 'Keccak' },
  { value: 'log', label: 'Log' },
  { value: 'account', label: 'Account' },
  { value: 'call', label: 'Call' },
  { value: 'context', label: 'Context' },
  { value: 'system', label: 'System' },
  // Precompiles
  { value: 'bn128', label: 'BN128' },
  { value: 'bls', label: 'BLS' },
  { value: 'precompile', label: 'Precompile' },
  // Other
  { value: 'scenario', label: 'Scenario' },
  { value: 'other', label: 'Other' },
]

const SORT_OPTIONS: { value: SortField; label: string; title?: string }[] = [
  { value: 'throughput', label: 'Throughput' },
  { value: 'execution', label: 'Execution Time' },
  { value: 'overhead', label: 'Overhead', title: 'state_read + state_hash + commit' },
  { value: 'order', label: 'Test #' },
  { value: 'name', label: 'Name' },
]

export function DashboardFilters({ state, stats, onUpdate }: DashboardFiltersProps) {
  const [isDropdownOpen, setIsDropdownOpen] = useState(false)
  const dropdownRef = useRef<HTMLDivElement>(null)

  // Close dropdown when clicking outside
  useEffect(() => {
    function handleClickOutside(event: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(event.target as Node)) {
        setIsDropdownOpen(false)
      }
    }
    document.addEventListener('mousedown', handleClickOutside)
    return () => document.removeEventListener('mousedown', handleClickOutside)
  }, [])

  const toggleCategory = (category: TestCategory) => {
    const currentCategories = state.categories
    const validCategories = currentCategories.filter((c) => ALL_CATEGORIES.includes(c as TestCategory))

    if (currentCategories.length === 0) {
      // Currently "all" selected, switching to exclude one category
      onUpdate({ categories: ALL_CATEGORIES.filter((c) => c !== category) })
    } else if (validCategories.length === 0) {
      // Currently "none" selected, start fresh with just this category
      onUpdate({ categories: [category] })
    } else if (currentCategories.includes(category)) {
      // Remove from selection
      const newCategories = currentCategories.filter((c) => c !== category)
      if (newCategories.length === 0) {
        // All categories unchecked, switch to "none" mode
        onUpdate({ categories: ['__none__' as TestCategory] })
      } else {
        onUpdate({ categories: newCategories })
      }
    } else {
      // Add to selection
      const newCategories = [...validCategories, category]
      // If all categories are now selected, switch back to empty (meaning "all")
      if (newCategories.length === ALL_CATEGORIES.length) {
        onUpdate({ categories: [] })
      } else {
        onUpdate({ categories: newCategories })
      }
    }
  }

  const selectAll = () => {
    onUpdate({ categories: [] })
  }

  const selectNone = () => {
    // Set to a single fake category that won't match anything
    // This effectively filters out everything
    onUpdate({ categories: ['__none__' as TestCategory] })
  }

  // Check if a category is a valid one (not the special '__none__' marker)
  const isValidCategory = (cat: string): cat is TestCategory => ALL_CATEGORIES.includes(cat as TestCategory)

  const getSelectedValidCategories = () => state.categories.filter(isValidCategory)

  const getCategoryLabel = () => {
    const validCategories = getSelectedValidCategories()
    if (state.categories.length === 0) {
      return 'All Categories'
    }
    if (validCategories.length === 0) {
      return 'None'
    }
    if (validCategories.length === 1) {
      const cat = CATEGORY_OPTIONS.find((o) => o.value === validCategories[0])
      return cat?.label ?? validCategories[0]
    }
    return `${validCategories.length} categories`
  }

  return (
    <div className="flex flex-wrap items-center gap-4 border-b border-gray-200 px-4 py-3 dark:border-gray-700">
      {/* Category Filter */}
      <div className="flex items-center gap-2">
        <span className="text-xs text-gray-500 dark:text-gray-400">Category:</span>
        <div className="relative" ref={dropdownRef}>
          <button
            onClick={() => setIsDropdownOpen(!isDropdownOpen)}
            className="flex items-center gap-2 rounded-sm border border-gray-300 bg-white px-3 py-1.5 text-left text-sm dark:border-gray-600 dark:bg-gray-700"
          >
            {getSelectedValidCategories().length === 1 && (
              <span
                className="size-2.5 rounded-full"
                style={{ backgroundColor: CATEGORY_COLORS[getSelectedValidCategories()[0]] }}
              />
            )}
            <span className="text-gray-900 dark:text-gray-100">{getCategoryLabel()}</span>
            <svg className="size-4 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
            </svg>
          </button>

          {isDropdownOpen && (
            <div className="absolute z-20 mt-1 w-56 rounded-sm border border-gray-200 bg-white py-1 shadow-lg dark:border-gray-600 dark:bg-gray-700">
              {/* Select All / None buttons */}
              <div className="flex gap-2 border-b border-gray-200 px-3 py-2 dark:border-gray-600">
                <button
                  onClick={selectAll}
                  className="text-xs text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  All
                </button>
                <span className="text-gray-300 dark:text-gray-600">|</span>
                <button
                  onClick={selectNone}
                  className="text-xs text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  None
                </button>
              </div>

              {/* Category checkboxes */}
              <div className="max-h-64 overflow-y-auto">
                {CATEGORY_OPTIONS.map((option) => {
                  const isSelected = state.categories.length === 0 || state.categories.includes(option.value)
                  const count = stats?.categoryBreakdown[option.value] ?? 0
                  const hasNoneSelected = state.categories.length > 0 && getSelectedValidCategories().length === 0

                  return (
                    <label
                      key={option.value}
                      className="flex cursor-pointer items-center gap-2 px-3 py-1.5 hover:bg-gray-100 dark:hover:bg-gray-600"
                    >
                      <input
                        type="checkbox"
                        checked={isSelected && !hasNoneSelected}
                        onChange={() => toggleCategory(option.value)}
                        className="rounded-xs border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600"
                      />
                      <span
                        className="size-2.5 rounded-full"
                        style={{ backgroundColor: CATEGORY_COLORS[option.value] }}
                      />
                      <span className="flex-1 text-sm text-gray-900 dark:text-gray-100">{option.label}</span>
                      <span className="text-xs text-gray-500 dark:text-gray-400">{count}</span>
                    </label>
                  )
                })}
              </div>
            </div>
          )}
        </div>
      </div>

      {/* Sort Options */}
      <div className="flex items-center gap-2">
        <span className="text-xs text-gray-500 dark:text-gray-400">Sort:</span>
        <select
          value={state.sortBy}
          onChange={(e) => onUpdate({ sortBy: e.target.value as SortField })}
          className="rounded-sm border border-gray-300 bg-white px-3 py-1.5 text-sm dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
        >
          {SORT_OPTIONS.map((option) => (
            <option key={option.value} value={option.value} title={option.title}>
              {option.label}
            </option>
          ))}
        </select>
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
