import type { TestCategory } from '../types'

export const CATEGORY_COLORS: Record<TestCategory, string> = {
  add: '#3b82f6', // blue
  mul: '#22c55e', // green
  pairing: '#f97316', // orange
  other: '#6b7280', // gray
}

export const COMPARISON_COLORS = ['#3b82f6', '#22c55e', '#f97316', '#ef4444', '#a855f7']

export const TIMING_COLORS = {
  execution: '#22c55e', // green
  stateRead: '#3b82f6', // blue
  stateHash: '#f97316', // orange
  commit: '#a855f7', // purple
}

export const CACHE_COLORS = {
  good: '#22c55e', // green (≥80%)
  poor: '#f97316', // orange (<80%)
  account: '#3b82f6', // blue
  storage: '#22c55e', // green
  code: '#a855f7', // purple
}
