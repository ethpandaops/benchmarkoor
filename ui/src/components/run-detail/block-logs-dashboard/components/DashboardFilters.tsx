import { useState, useRef, useEffect } from 'react'
import { ChevronDown } from 'lucide-react'
import type { DashboardState, DashboardStats, TestCategory } from '../types'
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
            <ChevronDown className="size-4 text-gray-400" />
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
                {CATEGORY_OPTIONS.filter((option) => (stats?.categoryBreakdown[option.value] ?? 0) > 0).map((option) => {
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
        {stats && (
          <div className="relative flex w-32 items-center">
            <input
              type="range"
              min={0}
              max={Math.ceil(stats.maxThroughput)}
              step={1}
              value={state.minThroughput ?? 0}
              onChange={(e) => {
                const val = Number(e.target.value)
                const maxVal = state.maxThroughput ?? Math.ceil(stats.maxThroughput)
                onUpdate({ minThroughput: val > 0 ? Math.min(val, maxVal) : undefined })
              }}
              className="pointer-events-none absolute h-1 w-full appearance-none rounded-sm bg-gray-200 dark:bg-gray-600 [&::-webkit-slider-thumb]:pointer-events-auto [&::-webkit-slider-thumb]:size-3 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-blue-500 [&::-webkit-slider-thumb]:hover:bg-blue-600"
            />
            <input
              type="range"
              min={0}
              max={Math.ceil(stats.maxThroughput)}
              step={1}
              value={state.maxThroughput ?? Math.ceil(stats.maxThroughput)}
              onChange={(e) => {
                const val = Number(e.target.value)
                const minVal = state.minThroughput ?? 0
                onUpdate({ maxThroughput: Math.max(val, minVal) })
              }}
              className="pointer-events-none absolute h-1 w-full appearance-none rounded-sm bg-transparent [&::-webkit-slider-thumb]:pointer-events-auto [&::-webkit-slider-thumb]:size-3 [&::-webkit-slider-thumb]:appearance-none [&::-webkit-slider-thumb]:rounded-full [&::-webkit-slider-thumb]:bg-blue-500 [&::-webkit-slider-thumb]:hover:bg-blue-600"
            />
          </div>
        )}
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
        {stats && stats.outlierCount > 0 && (
          <label className="flex cursor-pointer items-center gap-1.5 text-xs text-gray-600 dark:text-gray-400">
            <input
              type="checkbox"
              checked={state.excludeOutliers}
              onChange={(e) => onUpdate({ excludeOutliers: e.target.checked })}
              className="rounded-xs border-gray-300 text-blue-600 focus:ring-blue-500 dark:border-gray-600"
            />
            Exclude outliers ({stats.outlierCount})
          </label>
        )}
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
