import { useState } from 'react'
import clsx from 'clsx'
import { ChevronDown } from 'lucide-react'

interface CardProps {
  title: React.ReactNode
  children: React.ReactNode
  collapsible?: boolean
  defaultCollapsed?: boolean
  className?: string
  // headerExtra renders a summary on the right side of the header (before the
  // collapse chevron), e.g. a size or status shown even while collapsed.
  headerExtra?: React.ReactNode
}

export function Card({
  title,
  children,
  collapsible = false,
  defaultCollapsed = false,
  className,
  headerExtra,
}: CardProps) {
  const [isCollapsed, setIsCollapsed] = useState(defaultCollapsed)

  return (
    <div className={clsx('overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800', className)}>
      <div
        className={clsx(
          'flex items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 dark:border-gray-700',
          collapsible && 'cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700/50',
        )}
        onClick={collapsible ? () => setIsCollapsed(!isCollapsed) : undefined}
      >
        <h3 className="min-w-0 text-sm/6 font-medium text-gray-900 dark:text-gray-100">{title}</h3>
        {(headerExtra || collapsible) && (
          <div className="flex shrink-0 items-center gap-3">
            {headerExtra}
            {collapsible && (
              <ChevronDown
                className={clsx('size-5 text-gray-500 transition-transform', isCollapsed && '-rotate-90')}
              />
            )}
          </div>
        )}
      </div>
      {!isCollapsed && <div className="p-4">{children}</div>}
    </div>
  )
}
