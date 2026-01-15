import clsx from 'clsx'

type BadgeVariant = 'default' | 'success' | 'error' | 'warning' | 'info'

interface BadgeProps {
  children: React.ReactNode
  variant?: BadgeVariant
  className?: string
}

const variantClasses: Record<BadgeVariant, string> = {
  default: 'bg-gray-100 text-gray-800 dark:bg-gray-700 dark:text-gray-200',
  success: 'bg-green-100 text-green-800 dark:bg-green-900/50 dark:text-green-200',
  error: 'bg-red-100 text-red-800 dark:bg-red-900/50 dark:text-red-200',
  warning: 'bg-yellow-100 text-yellow-800 dark:bg-yellow-900/50 dark:text-yellow-200',
  info: 'bg-blue-100 text-blue-800 dark:bg-blue-900/50 dark:text-blue-200',
}

export function Badge({ children, variant = 'default', className }: BadgeProps) {
  return (
    <span
      className={clsx(
        'inline-flex items-center rounded-sm px-2 py-0.5 text-xs/5 font-medium',
        variantClasses[variant],
        className,
      )}
    >
      {children}
    </span>
  )
}
