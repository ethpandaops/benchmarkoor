interface EmptyStateProps {
  title: string
  message: string
  icon?: React.ReactNode
}

export function EmptyState({ title, message, icon }: EmptyStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-4 py-12">
      {icon ?? (
        <div className="flex size-12 items-center justify-center rounded-full bg-gray-100 dark:bg-gray-700">
          <svg className="size-6 text-gray-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M20 13V6a2 2 0 00-2-2H6a2 2 0 00-2 2v7m16 0v5a2 2 0 01-2 2H6a2 2 0 01-2-2v-5m16 0h-2.586a1 1 0 00-.707.293l-2.414 2.414a1 1 0 01-.707.293h-3.172a1 1 0 01-.707-.293l-2.414-2.414A1 1 0 006.586 13H4"
            />
          </svg>
        </div>
      )}
      <div className="text-center">
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">{title}</h3>
        <p className="mt-1 text-sm/6 text-gray-500 dark:text-gray-400">{message}</p>
      </div>
    </div>
  )
}
