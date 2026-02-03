import { useNavigate, useSearch } from '@tanstack/react-router'
import { useMemo, useCallback } from 'react'
import type { DashboardState, DashboardTab, SortField, SortOrder, TestCategory } from '../types'

interface BlockLogsDashboardSearch {
  blTab?: DashboardTab
  blCategory?: 'all' | TestCategory
  blSortBy?: SortField
  blSortOrder?: SortOrder
  blMinThroughput?: number
  blMaxThroughput?: number
  blExcludeOutliers?: boolean
  blLogScale?: boolean
  blSelected?: string
}

const DEFAULT_STATE: DashboardState = {
  activeTab: 'overview',
  category: 'all',
  sortBy: 'throughput',
  sortOrder: 'desc',
  excludeOutliers: true,
  useLogScale: false,
  selectedTests: [],
}

export function useDashboardState(runId: string) {
  const navigate = useNavigate()
  const search = useSearch({ from: '/runs/$runId' }) as BlockLogsDashboardSearch & Record<string, unknown>

  const state = useMemo<DashboardState>(() => ({
    activeTab: search.blTab ?? DEFAULT_STATE.activeTab,
    category: search.blCategory ?? DEFAULT_STATE.category,
    sortBy: search.blSortBy ?? DEFAULT_STATE.sortBy,
    sortOrder: search.blSortOrder ?? DEFAULT_STATE.sortOrder,
    minThroughput: search.blMinThroughput,
    maxThroughput: search.blMaxThroughput,
    excludeOutliers: search.blExcludeOutliers ?? DEFAULT_STATE.excludeOutliers,
    useLogScale: search.blLogScale ?? DEFAULT_STATE.useLogScale,
    selectedTests: search.blSelected ? search.blSelected.split(',').slice(0, 5) : DEFAULT_STATE.selectedTests,
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

    // Category
    if (newState.category !== DEFAULT_STATE.category) {
      newSearch.blCategory = newState.category
    } else {
      delete newSearch.blCategory
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

    // Selected tests
    if (newState.selectedTests.length > 0) {
      newSearch.blSelected = newState.selectedTests.join(',')
    } else {
      delete newSearch.blSelected
    }

    navigate({
      to: '/runs/$runId',
      params: { runId },
      search: newSearch,
    })
  }, [navigate, runId, search, state])

  const toggleTestSelection = useCallback((testName: string) => {
    const currentSelected = state.selectedTests
    const isSelected = currentSelected.includes(testName)

    let newSelected: string[]
    if (isSelected) {
      newSelected = currentSelected.filter((t) => t !== testName)
    } else if (currentSelected.length < 5) {
      newSelected = [...currentSelected, testName]
    } else {
      // Already at max, don't add
      return
    }

    updateState({ selectedTests: newSelected })
  }, [state.selectedTests, updateState])

  const clearSelection = useCallback(() => {
    updateState({ selectedTests: [] })
  }, [updateState])

  return {
    state,
    updateState,
    toggleTestSelection,
    clearSelection,
  }
}
