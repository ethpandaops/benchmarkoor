import { Link } from '@tanstack/react-router'

export function Header() {
  return (
    <header className="border-b border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
      <div className="mx-auto flex max-w-7xl items-center justify-between px-4 py-4">
        <Link to="/runs" search={{}} className="flex items-center gap-2">
          <svg className="size-8 text-blue-600 dark:text-blue-400" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path
              strokeLinecap="round"
              strokeLinejoin="round"
              strokeWidth={2}
              d="M9 19v-6a2 2 0 00-2-2H5a2 2 0 00-2 2v6a2 2 0 002 2h2a2 2 0 002-2zm0 0V9a2 2 0 012-2h2a2 2 0 012 2v10m-6 0a2 2 0 002 2h2a2 2 0 002-2m0 0V5a2 2 0 012-2h2a2 2 0 012 2v14a2 2 0 01-2 2h-2a2 2 0 01-2-2z"
            />
          </svg>
          <span className="text-lg/7 font-semibold text-gray-900 dark:text-gray-100">Benchmarkoor</span>
        </Link>
        <nav className="flex items-center gap-4">
          <Link
            to="/runs"
            search={{}}
            className="text-sm/6 font-medium text-gray-600 hover:text-gray-900 dark:text-gray-300 dark:hover:text-gray-100"
          >
            Runs
          </Link>
        </nav>
      </div>
    </header>
  )
}
