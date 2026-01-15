import { useState } from 'react'
import clsx from 'clsx'

interface CardProps {
  title: string
  children: React.ReactNode
  collapsible?: boolean
  defaultCollapsed?: boolean
  className?: string
}

export function Card({ title, children, collapsible = false, defaultCollapsed = false, className }: CardProps) {
  const [isCollapsed, setIsCollapsed] = useState(defaultCollapsed)

  return (
    <div className={clsx('overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800', className)}>
      <div
        className={clsx(
          'flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-gray-700',
          collapsible && 'cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700/50',
        )}
        onClick={collapsible ? () => setIsCollapsed(!isCollapsed) : undefined}
      >
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">{title}</h3>
        {collapsible && (
          <svg
            className={clsx('size-5 text-gray-500 transition-transform', isCollapsed && '-rotate-90')}
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
          >
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
          </svg>
        )}
      </div>
      {!isCollapsed && <div className="p-4">{children}</div>}
    </div>
  )
}
