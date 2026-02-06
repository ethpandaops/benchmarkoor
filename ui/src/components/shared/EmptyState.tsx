import { Inbox } from 'lucide-react'

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
          <Inbox className="size-6 text-gray-400" />
        </div>
      )}
      <div className="text-center">
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">{title}</h3>
        <p className="mt-1 text-sm/6 text-gray-500 dark:text-gray-400">{message}</p>
      </div>
    </div>
  )
}
