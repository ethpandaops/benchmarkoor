import clsx from 'clsx'

const STRATEGY_TOOLTIPS: Record<string, string> = {
  'none': 'No rollback: state is not reset between tests',
  'rpc-debug-setHead': 'RPC rollback: rewinds chain head via debug_setHead after each test',
  'container-recreate': 'Container recreate: stops and removes the container after each test, restarts from the same datadir',
  'container-checkpoint-restore': 'Checkpoint/restore: uses CRIU to snapshot and instantly restore the container between tests',
}

export function StrategyIcon({ strategy, className }: { strategy?: string; className?: string }) {
  const iconClass = clsx('size-4 text-gray-500 dark:text-gray-400', className)
  const tooltip = strategy ? STRATEGY_TOOLTIPS[strategy] ?? strategy : undefined

  switch (strategy) {
    case 'none':
      return (
        <svg className={iconClass} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5">
          <title>{tooltip}</title>
          <circle cx="8" cy="8" r="6" />
          <line x1="3.75" y1="12.25" x2="12.25" y2="3.75" />
        </svg>
      )
    case 'rpc-debug-setHead':
      return (
        <svg className={iconClass} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <title>{tooltip}</title>
          <path d="M4 7l-2 2 2 2" />
          <path d="M2 9h8a4 4 0 0 0 0-8H6" />
        </svg>
      )
    case 'container-recreate':
      return (
        <svg className={iconClass} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <title>{tooltip}</title>
          <path d="M13.5 6.5A5.5 5.5 0 0 0 3.08 4.5" />
          <path d="M2.5 9.5A5.5 5.5 0 0 0 12.92 11.5" />
          <path d="M3.08 4.5L1 3" />
          <path d="M3.08 4.5L5 3" />
          <path d="M12.92 11.5L15 13" />
          <path d="M12.92 11.5L11 13" />
        </svg>
      )
    case 'container-checkpoint-restore':
      return (
        <svg className={iconClass} viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round">
          <title>{tooltip}</title>
          <rect x="2" y="3" width="12" height="10" rx="1.5" />
          <circle cx="8" cy="8" r="2.5" />
          <circle cx="8" cy="8" r="0.75" fill="currentColor" stroke="none" />
        </svg>
      )
    default:
      return null
  }
}
