import { useNavigate, useSearch } from '@tanstack/react-router'
import { useMemo, useState, useEffect } from 'react'
import { useIndex } from '@/api/hooks/useIndex'
import { RunsTable } from '@/components/runs/RunsTable'
import { RunFilters } from '@/components/runs/RunFilters'
import { Pagination } from '@/components/shared/Pagination'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { EmptyState } from '@/components/shared/EmptyState'

const PAGE_SIZE = 20

export function RunsPage() {
  const navigate = useNavigate()
  const search = useSearch({ from: '/runs' }) as { page?: number; client?: string; sort?: 'newest' | 'oldest' }
  const { page = 1, client, sort = 'newest' } = search
  const { data: index, isLoading, error, refetch } = useIndex()
  const [localPage, setLocalPage] = useState(page)

  useEffect(() => {
    setLocalPage(page)
  }, [page])

  const clients = useMemo(() => {
    if (!index) return []
    const clientSet = new Set(index.entries.map((e) => e.instance.client))
    return Array.from(clientSet).sort()
  }, [index])

  const filteredAndSorted = useMemo(() => {
    if (!index) return []

    let entries = [...index.entries]

    if (client) {
      entries = entries.filter((e) => e.instance.client === client)
    }

    entries.sort((a, b) => {
      return sort === 'newest' ? b.timestamp - a.timestamp : a.timestamp - b.timestamp
    })

    return entries
  }, [index, client, sort])

  const totalPages = Math.ceil(filteredAndSorted.length / PAGE_SIZE)
  const paginatedEntries = filteredAndSorted.slice((localPage - 1) * PAGE_SIZE, localPage * PAGE_SIZE)

  const handlePageChange = (newPage: number) => {
    setLocalPage(newPage)
    navigate({ to: '/runs', search: { page: newPage, client, sort } })
  }

  const handleClientChange = (newClient: string | undefined) => {
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, client: newClient, sort } })
  }

  const handleSortChange = (newSort: 'newest' | 'oldest') => {
    navigate({ to: '/runs', search: { page: localPage, client, sort: newSort } })
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
        <RunFilters
          clients={clients}
          selectedClient={client}
          onClientChange={handleClientChange}
          sortOrder={sort}
          onSortChange={handleSortChange}
        />
      </div>

      {paginatedEntries.length === 0 ? (
        <EmptyState
          title="No matching runs"
          message={client ? `No runs found for client "${client}"` : 'No runs match your filters'}
        />
      ) : (
        <>
          <RunsTable entries={paginatedEntries} />
          {totalPages > 1 && (
            <div className="flex justify-center">
              <Pagination currentPage={localPage} totalPages={totalPages} onPageChange={handlePageChange} />
            </div>
          )}
        </>
      )}
    </div>
  )
}
