import { useNavigate, useSearch } from '@tanstack/react-router'
import { useMemo, useState, useEffect } from 'react'
import { useIndex } from '@/api/hooks/useIndex'
import { RunsTable, type SortColumn, type SortDirection } from '@/components/runs/RunsTable'
import { RunFilters } from '@/components/runs/RunFilters'
import { Pagination } from '@/components/shared/Pagination'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { EmptyState } from '@/components/shared/EmptyState'

const PAGE_SIZE_OPTIONS = [50, 100, 200] as const
const DEFAULT_PAGE_SIZE = 100

export function RunsPage() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/runs' }) as {
    page?: number
    pageSize?: number
    client?: string
    sortBy?: SortColumn
    sortDir?: SortDirection
  }
  const { page = 1, pageSize = DEFAULT_PAGE_SIZE, client, sortBy = 'timestamp', sortDir = 'desc' } = search
  const { data: index, isLoading, error, refetch } = useIndex()
  const [localPage, setLocalPage] = useState(page)
  const [localPageSize, setLocalPageSize] = useState(pageSize)

  useEffect(() => {
    setLocalPage(page)
  }, [page])

  useEffect(() => {
    setLocalPageSize(pageSize)
  }, [pageSize])

  const clients = useMemo(() => {
    if (!index) return []
    const clientSet = new Set(index.entries.map((e) => e.instance.client))
    return Array.from(clientSet).sort()
  }, [index])

  const filteredEntries = useMemo(() => {
    if (!index) return []
    if (!client) return index.entries
    return index.entries.filter((e) => e.instance.client === client)
  }, [index, client])

  const totalPages = Math.ceil(filteredEntries.length / localPageSize)
  const paginatedEntries = filteredEntries.slice((localPage - 1) * localPageSize, localPage * localPageSize)

  const handlePageChange = (newPage: number) => {
    setLocalPage(newPage)
    navigate({ to: '/runs', search: { page: newPage, pageSize: localPageSize, client, sortBy, sortDir } })
  }

  const handlePageSizeChange = (newSize: number) => {
    setLocalPageSize(newSize)
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: newSize, client, sortBy, sortDir } })
  }

  const handleClientChange = (newClient: string | undefined) => {
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: localPageSize, client: newClient, sortBy, sortDir } })
  }

  const handleSortChange = (newSortBy: SortColumn, newSortDir: SortDirection) => {
    navigate({ to: '/runs', search: { page: localPage, pageSize: localPageSize, client, sortBy: newSortBy, sortDir: newSortDir } })
  }

  if (isLoading) {
    return <LoadingState message="Loading runs..." />
  }

  if (error) {
    return <ErrorState message={error.message} retry={() => refetch()} />
  }

  if (!index || index.entries.length === 0) {
    return <EmptyState title="No runs found" message="No benchmark runs have been recorded yet." />
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <h1 className="text-2xl/8 font-bold text-gray-900 dark:text-gray-100">Benchmark Runs</h1>
        <RunFilters clients={clients} selectedClient={client} onClientChange={handleClientChange} />
      </div>

      {paginatedEntries.length === 0 ? (
        <EmptyState
          title="No matching runs"
          message={client ? `No runs found for client "${client}"` : 'No runs match your filters'}
        />
      ) : (
        <div className="flex flex-col gap-4">
          <RunsTable entries={paginatedEntries} sortBy={sortBy} sortDir={sortDir} onSortChange={handleSortChange} />
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <span className="text-sm/6 text-gray-500 dark:text-gray-400">Show</span>
              <select
                value={localPageSize}
                onChange={(e) => handlePageSizeChange(Number(e.target.value))}
                className="rounded-sm border border-gray-300 bg-white px-2 py-1 text-sm/6 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
              >
                {PAGE_SIZE_OPTIONS.map((size) => (
                  <option key={size} value={size}>
                    {size}
                  </option>
                ))}
              </select>
              <span className="text-sm/6 text-gray-500 dark:text-gray-400">per page</span>
            </div>
            {totalPages > 1 && (
              <Pagination currentPage={localPage} totalPages={totalPages} onPageChange={handlePageChange} />
            )}
          </div>
        </div>
      )}
    </div>
  )
}
