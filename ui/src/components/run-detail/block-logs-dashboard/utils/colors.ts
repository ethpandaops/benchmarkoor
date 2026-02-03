import type { TestCategory } from '../types'

export const ALL_CATEGORIES: TestCategory[] = [
  'arithmetic',
  'memory',
  'storage',
  'stack',
  'control',
  'keccak',
  'log',
  'account',
  'call',
  'context',
  'system',
  'bn128',
  'bls',
  'precompile',
  'scenario',
  'other',
]

export const CATEGORY_COLORS: Record<TestCategory, string> = {
  // EVM Instructions
  arithmetic: '#3b82f6', // blue
  memory: '#22c55e', // green
  storage: '#f97316', // orange
  stack: '#a855f7', // purple
  control: '#ec4899', // pink
  keccak: '#14b8a6', // teal
  log: '#eab308', // yellow
  account: '#06b6d4', // cyan
  call: '#8b5cf6', // violet
  context: '#f43f5e', // rose
  system: '#84cc16', // lime
  // Precompiles
  bn128: '#ef4444', // red
  bls: '#0ea5e9', // sky
  precompile: '#6366f1', // indigo
  // Other
  scenario: '#78716c', // stone
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
  good: '#22c55e', // green (>=80%)
  poor: '#f97316', // orange (<80%)
  account: '#3b82f6', // blue
  storage: '#22c55e', // green
  code: '#a855f7', // purple
  hit: '#22c55e', // green
  miss: '#ef4444', // red
}
