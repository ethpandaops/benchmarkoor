import type { RunConfig, RunResult } from '@/api/types'

export const MIN_COMPARE_RUNS = 2
export const MAX_COMPARE_RUNS = 4

export interface RunSlot {
  label: string
  color: string
  colorLight: string
  borderClass: string
  textClass: string
  textDarkClass: string
  bgDotClass: string
  diffTextClass: string
}

export const RUN_SLOTS: RunSlot[] = [
  {
    label: 'A',
    color: '#3b82f6',
    colorLight: '#60a5fa',
    borderClass: 'border-blue-500',
    textClass: 'text-blue-600',
    textDarkClass: 'text-blue-400',
    bgDotClass: 'bg-blue-500',
    diffTextClass: 'text-blue-700 dark:text-blue-300',
  },
  {
    label: 'B',
    color: '#f59e0b',
    colorLight: '#fbbf24',
    borderClass: 'border-amber-500',
    textClass: 'text-amber-600',
    textDarkClass: 'text-amber-400',
    bgDotClass: 'bg-amber-500',
    diffTextClass: 'text-amber-700 dark:text-amber-300',
  },
  {
    label: 'C',
    color: '#10b981',
    colorLight: '#34d399',
    borderClass: 'border-emerald-500',
    textClass: 'text-emerald-600',
    textDarkClass: 'text-emerald-400',
    bgDotClass: 'bg-emerald-500',
    diffTextClass: 'text-emerald-700 dark:text-emerald-300',
  },
  {
    label: 'D',
    color: '#8b5cf6',
    colorLight: '#a78bfa',
    borderClass: 'border-violet-500',
    textClass: 'text-violet-600',
    textDarkClass: 'text-violet-400',
    bgDotClass: 'bg-violet-500',
    diffTextClass: 'text-violet-700 dark:text-violet-300',
  },
]

export interface CompareRun {
  runId: string
  config: RunConfig
  result: RunResult | null
  index: number
}
