interface ErrorStateProps {
  title?: string
  message: string
  retry?: () => void
}

export function ErrorState({ title = 'Error', message, retry }: ErrorStateProps) {
  return (
    <div className="flex flex-col items-center justify-center gap-4 py-12">
      <div className="flex size-12 items-center justify-center rounded-full bg-red-100 dark:bg-red-900/50">
        <svg className="size-6 text-red-600 dark:text-red-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M12 9v2m0 4h.01m-6.938 4h13.856c1.54 0 2.502-1.667 1.732-3L13.732 4c-.77-1.333-2.694-1.333-3.464 0L3.34 16c-.77 1.333.192 3 1.732 3z"
          />
        </svg>
      </div>
      <div className="text-center">
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">{title}</h3>
        <p className="mt-1 text-sm/6 text-gray-500 dark:text-gray-400">{message}</p>
      </div>
      {retry && (
        <button
          onClick={retry}
          className="rounded-sm bg-blue-600 px-3 py-2 text-sm/6 font-medium text-white hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-600"
        >
          Try again
        </button>
      )}
    </div>
  )
}
