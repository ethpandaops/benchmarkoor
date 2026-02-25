import { useNavigate, useSearch } from '@tanstack/react-router'
import { SquareStack } from 'lucide-react'
import { useMemo, useState, useEffect, useCallback } from 'react'
import { useQueries } from '@tanstack/react-query'
import { useIndex } from '@/api/hooks/useIndex'
import { fetchData } from '@/api/client'
import type { SuiteInfo } from '@/api/types'
import { type IndexStepType, ALL_INDEX_STEP_TYPES, DEFAULT_INDEX_STEP_FILTER } from '@/api/types'
import { RunsTable } from '@/components/runs/RunsTable'
import { sortIndexEntries, type SortColumn, type SortDirection } from '@/components/runs/sortEntries'
import { RunFilters, type TestStatusFilter } from '@/components/runs/RunFilters'
import { Pagination } from '@/components/shared/Pagination'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { EmptyState } from '@/components/shared/EmptyState'
import { MAX_COMPARE_RUNS, MIN_COMPARE_RUNS } from '@/components/compare/constants'

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
    strategy?: string
    status?: TestStatusFilter
    sortBy?: SortColumn
    sortDir?: SortDirection
    steps?: string
  }
  const { page = 1, pageSize = DEFAULT_PAGE_SIZE, client, image, suite, strategy, status = 'all', sortBy = 'timestamp', sortDir = 'desc', steps } = search

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

  const strategies = useMemo(() => {
    if (!index) return []
    const strategySet = new Set(index.entries.map((e) => e.instance.rollback_strategy).filter((s): s is string => !!s))
    return Array.from(strategySet).sort()
  }, [index])

  const suiteHashes = useMemo(() => {
    if (!index) return []
    const suiteSet = new Set(index.entries.map((e) => e.suite_hash).filter((s): s is string => !!s))
    return Array.from(suiteSet).sort()
  }, [index])

  const suiteQueries = useQueries({
    queries: suiteHashes.map((hash) => ({
      queryKey: ['suite', hash],
      queryFn: async () => {
        const { data } = await fetchData<SuiteInfo>(`suites/${hash}/summary.json`, { cacheBustInterval: 3600 })
        return data
      },
      staleTime: Infinity,
    })),
  })

  const suites = useMemo(() => {
    return suiteHashes.map((hash, i) => {
      const name = suiteQueries[i]?.data?.metadata?.labels?.name
      return { hash, name }
    })
  }, [suiteHashes, suiteQueries])

  const filteredEntries = useMemo(() => {
    if (!index) return []
    return index.entries.filter((e) => {
      if (client && e.instance.client !== client) return false
      if (image && e.instance.image !== image) return false
      if (suite && e.suite_hash !== suite) return false
      if (strategy && e.instance.rollback_strategy !== strategy) return false
      if (status === 'passing' && e.tests.tests_total - e.tests.tests_passed > 0) return false
      if (status === 'failing' && e.tests.tests_total - e.tests.tests_passed === 0) return false
      return true
    })
  }, [index, client, image, suite, strategy, status])

  const sortedEntries = useMemo(() => sortIndexEntries(filteredEntries, sortBy, sortDir, stepFilter), [filteredEntries, sortBy, sortDir, stepFilter])
  const totalPages = Math.ceil(sortedEntries.length / localPageSize)
  const paginatedEntries = sortedEntries.slice((localPage - 1) * localPageSize, localPage * localPageSize)

  const handlePageChange = (newPage: number) => {
    setLocalPage(newPage)
    navigate({ to: '/runs', search: { page: newPage, pageSize: localPageSize, client, image, suite, strategy, status, sortBy, sortDir, steps } })
  }

  const handlePageSizeChange = (newSize: number) => {
    setLocalPageSize(newSize)
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: newSize, client, image, suite, strategy, status, sortBy, sortDir, steps } })
  }

  const handleClientChange = (newClient: string | undefined) => {
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: localPageSize, client: newClient, image, suite, strategy, status, sortBy, sortDir, steps } })
  }

  const handleImageChange = (newImage: string | undefined) => {
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: localPageSize, client, image: newImage, suite, strategy, status, sortBy, sortDir, steps } })
  }

  const handleSuiteChange = (newSuite: string | undefined) => {
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: localPageSize, client, image, suite: newSuite, strategy, status, sortBy, sortDir, steps } })
  }

  const handleStrategyChange = (newStrategy: string | undefined) => {
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: localPageSize, client, image, suite, strategy: newStrategy, status, sortBy, sortDir, steps } })
  }

  const handleStatusChange = (newStatus: TestStatusFilter) => {
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: localPageSize, client, image, suite, strategy, status: newStatus, sortBy, sortDir, steps } })
  }

  const handleSortChange = (newSortBy: SortColumn, newSortDir: SortDirection) => {
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: localPageSize, client, image, suite, strategy, status, sortBy: newSortBy, sortDir: newSortDir, steps } })
  }

  const handleStepFilterChange = (newFilter: IndexStepType[]) => {
    const stepsParam = newFilter.length === ALL_INDEX_STEP_TYPES.length ? undefined : newFilter.join(',')
    setLocalPage(1)
    navigate({ to: '/runs', search: { page: 1, pageSize: localPageSize, client, image, suite, strategy, status, sortBy, sortDir, steps: stepsParam } })
  }

  // Compare mode state
  const [compareMode, setCompareMode] = useState(false)
  const [selectedRunIds, setSelectedRunIds] = useState<Set<string>>(new Set())

  const handleSelectionChange = useCallback((runId: string, selected: boolean) => {
    setSelectedRunIds((prev) => {
      const next = new Set(prev)
      if (selected) {
        if (next.size >= MAX_COMPARE_RUNS) return prev
        next.add(runId)
      } else {
        next.delete(runId)
      }
      return next
    })
  }, [])

  const handleExitCompareMode = useCallback(() => {
    setCompareMode(false)
    setSelectedRunIds(new Set())
  }, [])

  // Suite match validation for selected runs
  const selectedSuiteInfo = useMemo(() => {
    if (selectedRunIds.size < MIN_COMPARE_RUNS || !index) return { match: false, canCompare: false }
    const ids = Array.from(selectedRunIds)
    const hashes = new Set(
      ids.map((id) => index.entries.find((e) => e.run_id === id)?.suite_hash).filter(Boolean),
    )
    const match = hashes.size === 1
    return { match, canCompare: true }
  }, [selectedRunIds, index])

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
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <h1 className="text-2xl/8 font-bold text-gray-900 dark:text-gray-100">Runs ({filteredEntries.length})</h1>
        <div className="flex flex-col items-end gap-2">
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
          <div className="flex flex-wrap items-end justify-end gap-4">
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
              strategies={strategies}
              selectedStrategy={strategy}
              onStrategyChange={handleStrategyChange}
              selectedStatus={status}
              onStatusChange={handleStatusChange}
            />
          </div>
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
            <div className="flex items-center gap-3">
              <button
                onClick={() => compareMode ? handleExitCompareMode() : setCompareMode(true)}
                className={`flex cursor-pointer items-center justify-center rounded-sm p-1.5 shadow-xs ring-1 ring-inset transition-colors ${
                  compareMode
                    ? 'bg-blue-600 text-white ring-blue-600 hover:bg-blue-700 hover:ring-blue-700'
                    : 'bg-white text-gray-500 ring-gray-300 hover:bg-gray-50 hover:text-gray-700 dark:bg-gray-800 dark:text-gray-400 dark:ring-gray-600 dark:hover:bg-gray-700 dark:hover:text-gray-200'
                }`}
                title="Compare"
              >
                <SquareStack className="size-4" />
              </button>
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
          <RunsTable entries={paginatedEntries} sortBy={sortBy} sortDir={sortDir} onSortChange={handleSortChange} showSuite stepFilter={stepFilter} selectable={compareMode} selectedRunIds={selectedRunIds} onSelectionChange={handleSelectionChange} />
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

      {compareMode && (
        <div className="fixed inset-x-0 bottom-0 z-50 border-t border-gray-200 bg-white px-6 py-3 shadow-sm dark:border-gray-700 dark:bg-gray-800">
          <div className="mx-auto flex max-w-7xl items-center justify-between">
            <div className="flex items-center gap-3">
              <span className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">
                {selectedRunIds.size} of {MAX_COMPARE_RUNS} selected
              </span>
              {selectedRunIds.size >= MIN_COMPARE_RUNS && !selectedSuiteInfo.match && (
                <span className="text-xs/5 text-yellow-600 dark:text-yellow-400">
                  Different suites — comparison may not be meaningful
                </span>
              )}
            </div>
            <div className="flex items-center gap-2">
              <button
                onClick={handleExitCompareMode}
                className="rounded-sm px-3 py-1.5 text-sm/6 font-medium text-gray-700 hover:bg-gray-100 dark:text-gray-300 dark:hover:bg-gray-700"
              >
                Cancel
              </button>
              <button
                disabled={selectedRunIds.size < MIN_COMPARE_RUNS}
                onClick={() => {
                  const ids = Array.from(selectedRunIds)
                  navigate({ to: '/compare', search: { runs: ids.join(',') } })
                }}
                className="rounded-sm bg-blue-600 px-4 py-1.5 text-sm/6 font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-50"
              >
                Compare
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
