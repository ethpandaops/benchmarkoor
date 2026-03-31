import type { RunConfig, RunResult } from '@/api/types'

export const MIN_COMPARE_RUNS = 2
export const MAX_COMPARE_RUNS = 5

export interface RunSlot {
  label: string
  color: string
  colorLight: string
  borderClass: string
  textClass: string
  textDarkClass: string
  bgDotClass: string
  diffTextClass: string
  badgeBgClass: string
  badgeTextClass: string
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
    badgeBgClass: 'bg-blue-100 dark:bg-blue-900/50',
    badgeTextClass: 'text-blue-700 dark:text-blue-300',
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
    badgeBgClass: 'bg-amber-100 dark:bg-amber-900/50',
    badgeTextClass: 'text-amber-700 dark:text-amber-300',
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
    badgeBgClass: 'bg-emerald-100 dark:bg-emerald-900/50',
    badgeTextClass: 'text-emerald-700 dark:text-emerald-300',
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
    badgeBgClass: 'bg-violet-100 dark:bg-violet-900/50',
    badgeTextClass: 'text-violet-700 dark:text-violet-300',
  },
  {
    label: 'E',
    color: '#ef4444',
    colorLight: '#f87171',
    borderClass: 'border-red-500',
    textClass: 'text-red-600',
    textDarkClass: 'text-red-400',
    bgDotClass: 'bg-red-500',
    diffTextClass: 'text-red-700 dark:text-red-300',
    badgeBgClass: 'bg-red-100 dark:bg-red-900/50',
    badgeTextClass: 'text-red-700 dark:text-red-300',
  },
]

export interface CompareRun {
  runId: string
  config: RunConfig
  result: RunResult | null
  index: number
}

export type LabelMode = string

export type ChartType = 'line' | 'bar' | 'dot'

export const CHART_TYPE_OPTIONS: { value: ChartType; label: string }[] = [
  { value: 'line', label: 'Line' },
  { value: 'bar', label: 'Bar' },
  { value: 'dot', label: 'Dot' },
]

export const LABEL_MODE_BASE_OPTIONS: { value: LabelMode; label: string }[] = [
  { value: 'none', label: 'None' },
  { value: 'instance-id', label: 'Instance ID' },
]

/** Build the full label mode options from the base options + label keys found in runs. */
export function buildLabelModeOptions(runs: CompareRun[]): { value: LabelMode; label: string }[] {
  const keys = new Set<string>()
  for (const run of runs) {
    if (run.config.metadata?.labels) {
      for (const key of Object.keys(run.config.metadata.labels)) {
        if (!key.startsWith('github.') && key !== 'name') keys.add(key)
      }
    }
  }
  const labelOptions = Array.from(keys).sort().map((key) => ({ value: `label:${key}`, label: key }))
  return [...LABEL_MODE_BASE_OPTIONS, ...labelOptions]
}

export function formatRunLabel(slot: RunSlot, run: CompareRun, labelMode: LabelMode): string {
  if (labelMode === 'instance-id' && run.config.instance.id) {
    return `${slot.label} (${run.config.instance.id})`
  }
  if (labelMode.startsWith('label:')) {
    const key = labelMode.slice(6)
    const value = run.config.metadata?.labels?.[key]
    if (value) return `${slot.label} (${value})`
  }
  return slot.label
}
