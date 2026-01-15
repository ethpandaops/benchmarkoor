import { useMemo, useState } from 'react'
import { Link, useParams, useNavigate, useSearch } from '@tanstack/react-router'
import { Tab, TabGroup, TabList, TabPanel, TabPanels } from '@headlessui/react'
import clsx from 'clsx'
import { useSuite } from '@/api/hooks/useSuite'
import { useIndex } from '@/api/hooks/useIndex'
import { SuiteSource } from '@/components/suite-detail/SuiteSource'
import { TestFilesList } from '@/components/suite-detail/TestFilesList'
import { RunsTable, type SortColumn, type SortDirection } from '@/components/runs/RunsTable'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { Badge } from '@/components/shared/Badge'
import { Pagination } from '@/components/shared/Pagination'

const PAGE_SIZE_OPTIONS = [50, 100, 200] as const
const DEFAULT_PAGE_SIZE = 100

export function SuiteDetailPage() {
  const { suiteHash } = useParams({ from: '/suites/$suiteHash' })
  const navigate = useNavigate()
  const search = useSearch({ from: '/suites/$suiteHash' }) as {
    tab?: string
    sortBy?: SortColumn
    sortDir?: SortDirection
  }
  const { tab, sortBy = 'timestamp', sortDir = 'desc' } = search
  const { data: suite, isLoading, error, refetch } = useSuite(suiteHash)
  const { data: index } = useIndex()
  const [runsPage, setRunsPage] = useState(1)
  const [runsPageSize, setRunsPageSize] = useState(DEFAULT_PAGE_SIZE)

  const suiteRuns = useMemo(() => {
    if (!index) return []
    return index.entries.filter((entry) => entry.suite_hash === suiteHash)
  }, [index, suiteHash])

  const totalRunsPages = Math.ceil(suiteRuns.length / runsPageSize)
  const paginatedRuns = suiteRuns.slice((runsPage - 1) * runsPageSize, runsPage * runsPageSize)

  const handleRunsPageSizeChange = (newSize: number) => {
    setRunsPageSize(newSize)
    setRunsPage(1)
  }

  if (isLoading) {
    return <LoadingState message="Loading suite details..." />
  }

  if (error) {
    return <ErrorState message={error.message} retry={() => refetch()} />
  }

  if (!suite) {
    return <ErrorState message="Suite not found" />
  }

  const hasWarmup = suite.warmup && suite.warmup.length > 0
  const warmupTabIndex = hasWarmup ? 2 : -1

  const getTabIndex = () => {
    if (tab === 'tests') return 1
    if (tab === 'warmup' && hasWarmup) return warmupTabIndex
    return 0 // runs is default
  }

  const handleTabChange = (index: number) => {
    let newTab: string
    if (index === 0) {
      newTab = 'runs'
    } else if (index === 1) {
      newTab = 'tests'
    } else {
      newTab = 'warmup'
    }
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab: newTab, sortBy, sortDir },
    })
  }

  const handleSortChange = (newSortBy: SortColumn, newSortDir: SortDirection) => {
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab, sortBy: newSortBy, sortDir: newSortDir },
    })
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="flex items-center gap-2 text-sm/6 text-gray-500 dark:text-gray-400">
        <Link to="/suites" className="hover:text-gray-700 dark:hover:text-gray-300">
          Suites
        </Link>
        <span>/</span>
        <span className="font-mono text-gray-900 dark:text-gray-100">{suiteHash}</span>
      </div>

      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div className="flex flex-col gap-2">
          <h1 className="font-mono text-2xl/8 font-bold text-gray-900 dark:text-gray-100">{suite.hash}</h1>
        </div>
        <div className="flex flex-wrap items-center gap-3">
          {suite.filter && <Badge variant="info">Filter: {suite.filter}</Badge>}
          <Badge variant="default">{suite.tests.length} tests</Badge>
          {hasWarmup && <Badge variant="warning">{suite.warmup!.length} warmup</Badge>}
        </div>
      </div>

      <TabGroup selectedIndex={getTabIndex()} onChange={handleTabChange}>
        <TabList className="flex gap-1 rounded-sm bg-gray-100 p-1 dark:bg-gray-800">
          <Tab
            className={({ selected }) =>
              clsx(
                'rounded-sm px-4 py-2 text-sm/6 font-medium transition-colors focus:outline-hidden',
                selected
                  ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-700 dark:text-gray-100'
                  : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
              )
            }
          >
            Runs ({suiteRuns.length})
          </Tab>
          <Tab
            className={({ selected }) =>
              clsx(
                'rounded-sm px-4 py-2 text-sm/6 font-medium transition-colors focus:outline-hidden',
                selected
                  ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-700 dark:text-gray-100'
                  : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
              )
            }
          >
            Tests ({suite.tests.length})
          </Tab>
          {hasWarmup && (
            <Tab
              className={({ selected }) =>
                clsx(
                  'rounded-sm px-4 py-2 text-sm/6 font-medium transition-colors focus:outline-hidden',
                  selected
                    ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-700 dark:text-gray-100'
                    : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                )
              }
            >
              Warmup ({suite.warmup!.length})
            </Tab>
          )}
        </TabList>
        <TabPanels className="mt-4">
          <TabPanel>
            {suiteRuns.length === 0 ? (
              <p className="py-8 text-center text-sm/6 text-gray-500 dark:text-gray-400">
                No runs found for this suite.
              </p>
            ) : (
              <div className="flex flex-col gap-4">
                <RunsTable entries={paginatedRuns} showSuite={false} sortBy={sortBy} sortDir={sortDir} onSortChange={handleSortChange} />
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <span className="text-sm/6 text-gray-500 dark:text-gray-400">Show</span>
                    <select
                      value={runsPageSize}
                      onChange={(e) => handleRunsPageSizeChange(Number(e.target.value))}
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
                  {totalRunsPages > 1 && (
                    <Pagination currentPage={runsPage} totalPages={totalRunsPages} onPageChange={setRunsPage} />
                  )}
                </div>
              </div>
            )}
          </TabPanel>
          <TabPanel className="flex flex-col gap-4">
            {suite.source.tests && <SuiteSource title="Tests Source" source={suite.source.tests} />}
            <TestFilesList title="Tests" files={suite.tests} suiteHash={suiteHash} type="tests" />
          </TabPanel>
          {hasWarmup && (
            <TabPanel className="flex flex-col gap-4">
              {suite.source.warmup && <SuiteSource title="Warmup Source" source={suite.source.warmup} />}
              <TestFilesList title="Warmup Tests" files={suite.warmup!} suiteHash={suiteHash} type="warmup" />
            </TabPanel>
          )}
        </TabPanels>
      </TabGroup>
    </div>
  )
}
