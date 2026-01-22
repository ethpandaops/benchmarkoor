import clsx from 'clsx'
import type { RunStatus } from '@/api/types'

interface StatusBadgeProps {
  status?: RunStatus
  showCompleted?: boolean
  className?: string
}

const statusConfig: Record<RunStatus, { label: string; className: string; icon: React.ReactNode }> = {
  completed: {
    label: 'Completed',
    className: 'bg-green-100 text-green-800 dark:bg-green-900/50 dark:text-green-200',
    icon: (
      <svg className="size-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
      </svg>
    ),
  },
  container_died: {
    label: 'Container Died',
    className: 'bg-red-100 text-red-800 dark:bg-red-900/50 dark:text-red-200',
    icon: (
      <svg className="size-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path
          strokeLinecap="round"
          strokeLinejoin="round"
          strokeWidth={2}
          d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
        />
      </svg>
    ),
  },
  cancelled: {
    label: 'Cancelled',
    className: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/50 dark:text-yellow-200',
    icon: (
      <svg className="size-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M6 18L18 6M6 6l12 12" />
      </svg>
    ),
  },
}

export function StatusBadge({ status, showCompleted = false, className }: StatusBadgeProps) {
  // If no status or completed and we don't want to show completed, return null
  if (!status || (status === 'completed' && !showCompleted)) {
    return null
  }

  const config = statusConfig[status]

  return (
    <span
      className={clsx(
        'inline-flex items-center gap-1 rounded-sm px-2 py-0.5 text-xs/5 font-medium',
        config.className,
        className,
      )}
    >
      {config.icon}
      {config.label}
    </span>
  )
}

interface StatusAlertProps {
  status?: RunStatus
  terminationReason?: string
  containerExitCode?: number
}

export function StatusAlert({ status, terminationReason, containerExitCode }: StatusAlertProps) {
  // Only show alert for non-completed statuses
  if (!status || status === 'completed') {
    return null
  }

  const config = statusConfig[status]

  const alertClasses = {
    container_died: 'border-red-200 bg-red-50 dark:border-red-800 dark:bg-red-900/20',
    cancelled: 'border-yellow-200 bg-yellow-50 dark:border-yellow-800 dark:bg-yellow-900/20',
    completed: '',
  }

  const iconClasses = {
    container_died: 'text-red-600 dark:text-red-400',
    cancelled: 'text-yellow-600 dark:text-yellow-400',
    completed: '',
  }

  const textClasses = {
    container_died: 'text-red-800 dark:text-red-200',
    cancelled: 'text-yellow-800 dark:text-yellow-200',
    completed: '',
  }

  return (
    <div className={clsx('flex items-start gap-3 rounded-sm border p-4', alertClasses[status])}>
      <div className={clsx('shrink-0', iconClasses[status])}>{config.icon}</div>
      <div className="flex-1">
        <h3 className={clsx('text-sm/6 font-medium', textClasses[status])}>{config.label}</h3>
        {terminationReason && (
          <p className={clsx('mt-1 text-sm/6', textClasses[status])}>{terminationReason}</p>
        )}
        {containerExitCode !== undefined && (
          <p className={clsx('mt-1 text-sm/6', textClasses[status])}>
            Container exit code: <span className="font-mono font-semibold">{containerExitCode}</span>
          </p>
        )}
        <p className={clsx('mt-2 text-xs/5 opacity-75', textClasses[status])}>
          Partial results may have been collected before termination.
        </p>
      </div>
    </div>
  )
}
