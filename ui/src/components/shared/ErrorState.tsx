import { AlertTriangle } from 'lucide-react'

interface ErrorStateProps {
  title?: string
  message: string
  retry?: () => void
}

export function ErrorState({ title = 'Error', message, retry }: ErrorStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-4 py-12">
      <div className="flex size-12 items-center justify-center rounded-full bg-red-100 dark:bg-red-900/50">
        <AlertTriangle className="size-6 text-red-600 dark:text-red-400" />
      </div>
      <div className="text-center">
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">{title}</h3>
        <p className="mt-1 text-sm/6 text-gray-500 dark:text-gray-400">{message}</p>
      </div>
      {retry && (
        <button
          onClick={retry}
          className="rounded-sm bg-gray-800 px-3 py-2 text-sm/6 font-medium text-white hover:bg-gray-900 dark:bg-gray-100 dark:text-gray-900 dark:hover:bg-gray-200"
        >
          Try again
        </button>
      )}
    </div>
  )
}
