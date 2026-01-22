import { useNavigate, useSearch } from '@tanstack/react-router'
import { useMemo, useState, useEffect } from 'react'
import { useIndex } from '@/api/hooks/useIndex'
import { type IndexStepType, ALL_INDEX_STEP_TYPES, DEFAULT_INDEX_STEP_FILTER, getIndexAggregatedStats } from '@/api/types'
import { RunsTable, type SortColumn, type SortDirection } from '@/components/runs/RunsTable'
import { RunFilters, type TestStatusFilter } from '@/components/runs/RunFilters'
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
    image?: string
    suite?: string
    status?: TestStatusFilter
    sortBy?: SortColumn
    sortDir?: SortDirection
    steps?: string
  }
  const { page = 1, pageSize = DEFAULT_PAGE_SIZE, client, image, suite, status = 'all', sortBy = 'timestamp', sortDir = 'desc', steps } = search

  // Parse step filter from URL
  const parseStepFilter = (stepsParam: string | undefined): IndexStepType[] => {
    if (!stepsParam) return DEFAULT_INDEX_STEP_FILTER
    const parsed = stepsParam.split(',').filter((s): s is IndexStepType => ALL_INDEX_STEP_TYPES.includes(s as IndexStepType))
    return parsed.length > 0 ? parsed : DEFAULT_INDEX_STEP_FILTER
  }

  const stepFilter = parseStepFilter(steps)
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

  const images = useMemo(() => {
    if (!index) return []
    const imageSet = new Set(index.entries.map((e) => e.instance.image))
    return Array.from(imageSet).sort()
  }, [index])

  const suites = useMemo(() => {
    if (!index) return []
    const suiteSet = new Set(index.entries.map((e) => e.suite_hash).filter((s): s is string => !!s))
    return Array.from(suiteSet).sort()
  }, [index])

  const filteredEntries = useMemo(() => {
    if (!index) return []
    return index.entries.filter((e) => {
      if (client && e.instance.client !== client) return false
      if (image && e.instance.image !== image) return false
      if (suite && e.suite_hash !== suite) return false
      const stats = getIndexAggregatedStats(e, stepFilter)
      if (status === 'passing' && stats.fail > 0) return false
      if (status === 'failing' && stats.fail === 0) return false
      return true
    })
  }, [index, client, image, suite, status, stepFilter])

  const totalPages = Math.ceil(filteredEntries.length / localPageSize)
  const paginatedEntries = filteredEntries.slice((localPage - 1) * localPageSize, localPage * localPageSize)

  const handlePageChange = (newPage: number) => {
    setLocalPage(newPage)
    navigate({ to: '/runs', search: { page: newPage, pageSize: localPageSize, client, image, suite, status, sortBy, sortDir, steps } })
  }

  const handlePageSizeChange = (newSize: number) => {
    setLocalPageSize(newSize)
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: newSize, client, image, suite, status, sortBy, sortDir, steps } })
  }

  const handleClientChange = (newClient: string | undefined) => {
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: localPageSize, client: newClient, image, suite, status, sortBy, sortDir, steps } })
  }

  const handleImageChange = (newImage: string | undefined) => {
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: localPageSize, client, image: newImage, suite, status, sortBy, sortDir, steps } })
  }

  const handleSuiteChange = (newSuite: string | undefined) => {
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: localPageSize, client, image, suite: newSuite, status, sortBy, sortDir, steps } })
  }

  const handleStatusChange = (newStatus: TestStatusFilter) => {
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: localPageSize, client, image, suite, status: newStatus, sortBy, sortDir, steps } })
  }

  const handleSortChange = (newSortBy: SortColumn, newSortDir: SortDirection) => {
    navigate({ to: '/runs', search: { page: localPage, pageSize: localPageSize, client, image, suite, status, sortBy: newSortBy, sortDir: newSortDir, steps } })
  }

  const handleStepFilterChange = (newFilter: IndexStepType[]) => {
    const stepsParam = newFilter.length === ALL_INDEX_STEP_TYPES.length ? undefined : newFilter.join(',')
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: localPageSize, client, image, suite, status, sortBy, sortDir, steps: stepsParam } })
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
        <h1 className="text-2xl/8 font-bold text-gray-900 dark:text-gray-100">Benchmark Runs ({filteredEntries.length})</h1>
        <div className="flex flex-wrap items-center gap-4">
          <div className="flex items-center gap-2">
            <span className="text-sm/6 font-medium text-gray-700 dark:text-gray-300">Metric steps:</span>
            <div className="flex items-center gap-1">
              {ALL_INDEX_STEP_TYPES.map((step) => (
                <button
                  key={step}
                  onClick={() => {
                    const newFilter = stepFilter.includes(step)
                      ? stepFilter.filter((s) => s !== step)
                      : [...stepFilter, step]
                    if (newFilter.length > 0) {
                      handleStepFilterChange(newFilter)
                    }
                  }}
                  className={`rounded-sm px-2.5 py-1 text-xs font-medium capitalize transition-colors ${
                    stepFilter.includes(step)
                      ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300'
                      : 'bg-gray-100 text-gray-400 dark:bg-gray-700 dark:text-gray-500'
                  }`}
                >
                  {step}
                </button>
              ))}
            </div>
          </div>
          <RunFilters
            clients={clients}
            selectedClient={client}
            onClientChange={handleClientChange}
            images={images}
            selectedImage={image}
            onImageChange={handleImageChange}
            suites={suites}
            selectedSuite={suite}
            onSuiteChange={handleSuiteChange}
            selectedStatus={status}
            onStatusChange={handleStatusChange}
          />
        </div>
      </div>

      {paginatedEntries.length === 0 ? (
        <EmptyState
          title="No matching runs"
          message={client ? `No runs found for client "${client}"` : 'No runs match your filters'}
        />
      ) : (
        <div className="flex flex-col gap-4">
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
          <RunsTable entries={paginatedEntries} sortBy={sortBy} sortDir={sortDir} onSortChange={handleSortChange} showSuite stepFilter={stepFilter} />
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
