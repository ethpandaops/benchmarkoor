import { useNavigate, useSearch } from '@tanstack/react-router'
import { useMemo, useCallback } from 'react'
import type { DashboardState, DashboardTab, SortField, SortOrder, TestCategory } from '../types'
import { ALL_CATEGORIES } from '../utils/colors'

interface BlockLogsDashboardSearch {
  blTab?: DashboardTab
  blCategories?: string // Comma-separated list of categories
  blSortBy?: SortField
  blSortOrder?: SortOrder
  blMinThroughput?: number
  blMaxThroughput?: number
  blExcludeOutliers?: boolean
  blLogScale?: boolean
}

const DEFAULT_STATE: DashboardState = {
  activeTab: 'overview',
  categories: [], // Empty means all
  sortBy: 'throughput',
  sortOrder: 'asc',
  excludeOutliers: true,
  useLogScale: false,
}

function parseCategories(value: string | undefined): TestCategory[] {
  if (!value) return []
  // Preserve the special '__none__' sentinel value that indicates "no categories selected"
  if (value === '__none__') return ['__none__' as TestCategory]
  const categories = value.split(',').filter((c): c is TestCategory =>
    ALL_CATEGORIES.includes(c as TestCategory)
  )
  return categories
}

export function useDashboardState(runId: string) {
  const navigate = useNavigate()
  const search = useSearch({ from: '/runs/$runId' }) as BlockLogsDashboardSearch & Record<string, unknown>

  const state = useMemo<DashboardState>(() => ({
    activeTab: search.blTab ?? DEFAULT_STATE.activeTab,
    categories: parseCategories(search.blCategories),
    sortBy: search.blSortBy ?? DEFAULT_STATE.sortBy,
    sortOrder: search.blSortOrder ?? DEFAULT_STATE.sortOrder,
    minThroughput: search.blMinThroughput,
    maxThroughput: search.blMaxThroughput,
    excludeOutliers: search.blExcludeOutliers ?? DEFAULT_STATE.excludeOutliers,
    useLogScale: search.blLogScale ?? DEFAULT_STATE.useLogScale,
  }), [search])

  const updateState = useCallback((updates: Partial<DashboardState>) => {
    const newState = { ...state, ...updates }

    // Build new search params, only including non-default values
    const newSearch: Record<string, unknown> = { ...search }

    // Tab
    if (newState.activeTab !== DEFAULT_STATE.activeTab) {
      newSearch.blTab = newState.activeTab
    } else {
      delete newSearch.blTab
    }

    // Categories
    if (newState.categories.length > 0) {
      newSearch.blCategories = newState.categories.join(',')
    } else {
      delete newSearch.blCategories
    }

    // Sort
    if (newState.sortBy !== DEFAULT_STATE.sortBy) {
      newSearch.blSortBy = newState.sortBy
    } else {
      delete newSearch.blSortBy
    }

    if (newState.sortOrder !== DEFAULT_STATE.sortOrder) {
      newSearch.blSortOrder = newState.sortOrder
    } else {
      delete newSearch.blSortOrder
    }

    // Throughput range
    if (newState.minThroughput !== undefined) {
      newSearch.blMinThroughput = newState.minThroughput
    } else {
      delete newSearch.blMinThroughput
    }

    if (newState.maxThroughput !== undefined) {
      newSearch.blMaxThroughput = newState.maxThroughput
    } else {
      delete newSearch.blMaxThroughput
    }

    // Outliers
    if (newState.excludeOutliers !== DEFAULT_STATE.excludeOutliers) {
      newSearch.blExcludeOutliers = newState.excludeOutliers
    } else {
      delete newSearch.blExcludeOutliers
    }

    // Log scale
    if (newState.useLogScale !== DEFAULT_STATE.useLogScale) {
      newSearch.blLogScale = newState.useLogScale
    } else {
      delete newSearch.blLogScale
    }

    navigate({
      to: '/runs/$runId',
      params: { runId },
      search: newSearch,
    })
  }, [navigate, runId, search, state])

  return {
    state,
    updateState,
  }
}
