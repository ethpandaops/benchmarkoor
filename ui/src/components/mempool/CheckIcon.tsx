import clsx from 'clsx'
import { Check, TriangleAlert, X } from 'lucide-react'
import { SEVERITY_HELP, type CheckState } from './mempoolUtils'

const ICON = { passed: Check, 'soft-fail': TriangleAlert, 'hard-fail': X }

const ICON_COLOR: Record<CheckState, string> = {
  passed: 'text-green-600 dark:text-green-400',
  'soft-fail': 'text-amber-600 dark:text-amber-400',
  'hard-fail': 'text-red-600 dark:text-red-400',
}

const CHIP_COLOR: Record<CheckState, string> = {
  passed: 'bg-green-50 text-green-700 dark:bg-green-900/20 dark:text-green-400',
  'soft-fail': 'bg-amber-50 text-amber-700 ring-1 ring-amber-200 dark:bg-amber-900/30 dark:text-amber-300 dark:ring-amber-800/60',
  'hard-fail': 'bg-red-50 text-red-700 ring-1 ring-red-200 dark:bg-red-900/30 dark:text-red-300 dark:ring-red-800/60',
}

const CHECK_STATE_LABEL: Record<CheckState, string> = {
  passed: 'passed',
  'soft-fail': 'finding (soft check failed)',
  'hard-fail': 'failed (hard check)',
}

// CheckIcon is the status mark of a check: a green check, an amber warning
// for a finding, a red cross for a hard failure.
export function CheckIcon({ state, className }: { state: CheckState; className?: string }) {
  const Icon = ICON[state]
  return <Icon aria-label={CHECK_STATE_LABEL[state]} className={clsx('size-4 shrink-0', ICON_COLOR[state], className)} />
}

// CheckChip is CheckIcon on a small tinted square, for table cells.
export function CheckChip({ state, size = 'md', title }: { state: CheckState; size?: 'sm' | 'md'; title?: string }) {
  const Icon = ICON[state]
  return (
    <span
      title={title ?? CHECK_STATE_LABEL[state]}
      className={clsx('inline-flex items-center justify-center rounded-xs', size === 'sm' ? 'size-5' : 'size-6', CHIP_COLOR[state])}
    >
      <Icon aria-label={CHECK_STATE_LABEL[state]} className={size === 'sm' ? 'size-3' : 'size-3.5'} />
    </span>
  )
}

// SeverityTag is a small neutral "hard" / "soft" tag next to a check name.
// Hovering it explains the severity.
export function SeverityTag({ severity }: { severity: 'hard' | 'soft' }) {
  return (
    <span
      title={SEVERITY_HELP[severity]}
      className="cursor-help rounded-xs border border-gray-200 px-1 text-xs/4 text-gray-500 dark:border-gray-600 dark:text-gray-400"
    >
      {severity}
    </span>
  )
}
