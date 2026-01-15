import { useMemo, useState, useEffect } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useIndex } from '@/api/hooks/useIndex'
import { SuitesTable, type SuiteSortColumn, type SuiteSortDirection } from '@/components/suites/SuitesTable'
import { Pagination } from '@/components/shared/Pagination'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { EmptyState } from '@/components/shared/EmptyState'

const PAGE_SIZE = 20

export function SuitesPage() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/suites' }) as {
    page?: number
    sortBy?: SuiteSortColumn
    sortDir?: SuiteSortDirection
  }
  const { page = 1, sortBy = 'lastRun', sortDir = 'desc' } = search
  const { data: index, isLoading, error, refetch } = useIndex()
  const [currentPage, setCurrentPage] = useState(page)

  useEffect(() => {
    setCurrentPage(page)
  }, [page])

  const suites = useMemo(() => {
    if (!index) return []

    const suiteMap = new Map<string, { runCount: number; lastRun: number }>()
    for (const entry of index.entries) {
      if (entry.suite_hash) {
        const existing = suiteMap.get(entry.suite_hash)
        if (existing) {
          existing.runCount++
          existing.lastRun = Math.max(existing.lastRun, entry.timestamp)
        } else {
          suiteMap.set(entry.suite_hash, { runCount: 1, lastRun: entry.timestamp })
        }
      }
    }

    return Array.from(suiteMap.entries()).map(([hash, { runCount, lastRun }]) => ({ hash, runCount, lastRun }))
  }, [index])

  const totalPages = Math.ceil(suites.length / PAGE_SIZE)
  const paginatedSuites = suites.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE)

  const handlePageChange = (newPage: number) => {
    setCurrentPage(newPage)
    navigate({ to: '/suites', search: { page: newPage, sortBy, sortDir } })
  }

  const handleSortChange = (newSortBy: SuiteSortColumn, newSortDir: SuiteSortDirection) => {
    navigate({ to: '/suites', search: { page: currentPage, sortBy: newSortBy, sortDir: newSortDir } })
  }

  if (isLoading) {
    return <LoadingState message="Loading suites..." />
  }

  if (error) {
    return <ErrorState message={error.message} retry={() => refetch()} />
  }

  if (suites.length === 0) {
    return <EmptyState title="No suites found" message="No test suites have been used yet." />
  }

  return (
    <div className="flex flex-col gap-6">
      <h1 className="text-2xl/8 font-bold text-gray-900 dark:text-gray-100">Test Suites ({suites.length})</h1>

      <SuitesTable suites={paginatedSuites} sortBy={sortBy} sortDir={sortDir} onSortChange={handleSortChange} />

      {totalPages > 1 && (
        <div className="flex justify-center">
          <Pagination currentPage={currentPage} totalPages={totalPages} onPageChange={handlePageChange} />
        </div>
      )}
    </div>
  )
}
