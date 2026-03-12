import clsx from 'clsx'
import { ChevronLeft, ChevronRight } from 'lucide-react'

interface PaginationProps {
  currentPage: number
  totalPages: number
  onPageChange: (page: number) => void
}

export function Pagination({ currentPage, totalPages, onPageChange }: PaginationProps) {
  if (totalPages <= 1) return null

  const pages: (number | 'ellipsis')[] = []
  const delta = 2

  for (let i = 1; i <= totalPages; i++) {
    if (i === 1 || i === totalPages || (i >= currentPage - delta && i <= currentPage + delta)) {
      pages.push(i)
    } else if (pages[pages.length - 1] !== 'ellipsis') {
      pages.push('ellipsis')
    }
  }

  return (
    <nav className="flex items-center justify-center gap-0.5 sm:gap-1" aria-label="Pagination">
      <button
        onClick={() => onPageChange(currentPage - 1)}
        disabled={currentPage === 1}
        className={clsx(
          'flex size-7 items-center justify-center rounded-xs text-sm/6 sm:size-8',
          currentPage === 1
            ? 'cursor-not-allowed text-gray-300 dark:text-gray-600'
            : 'text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-700',
        )}
        aria-label="Previous page"
      >
        <ChevronLeft className="size-4 sm:size-5" />
      </button>

      {pages.map((page, index) =>
        page === 'ellipsis' ? (
          <span key={`ellipsis-${index}`} className="flex size-7 items-center justify-center text-gray-400 sm:size-8">
            ...
          </span>
        ) : (
          <button
            key={page}
            onClick={() => onPageChange(page)}
            className={clsx(
              'flex size-7 items-center justify-center rounded-xs text-xs/5 font-medium sm:size-8 sm:text-sm/6',
              page === currentPage
                ? 'bg-blue-600 text-white dark:bg-blue-500'
                : 'text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-700',
            )}
            aria-current={page === currentPage ? 'page' : undefined}
          >
            {page}
          </button>
        ),
      )}

      <button
        onClick={() => onPageChange(currentPage + 1)}
        disabled={currentPage === totalPages}
        className={clsx(
          'flex size-7 items-center justify-center rounded-xs text-sm/6 sm:size-8',
          currentPage === totalPages
            ? 'cursor-not-allowed text-gray-300 dark:text-gray-600'
            : 'text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-700',
        )}
        aria-label="Next page"
      >
        <ChevronRight className="size-4 sm:size-5" />
      </button>
    </nav>
  )
}
