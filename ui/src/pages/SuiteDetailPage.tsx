import { Link, useParams, useNavigate, useSearch } from '@tanstack/react-router'
import { Tab, TabGroup, TabList, TabPanel, TabPanels } from '@headlessui/react'
import clsx from 'clsx'
import { useSuite } from '@/api/hooks/useSuite'
import { SuiteSource } from '@/components/suite-detail/SuiteSource'
import { TestFilesList } from '@/components/suite-detail/TestFilesList'
import { LoadingState } from '@/components/shared/Spinner'
import { ErrorState } from '@/components/shared/ErrorState'
import { Badge } from '@/components/shared/Badge'

export function SuiteDetailPage() {
  const { suiteHash } = useParams({ from: '/suites/$suiteHash' })
  const navigate = useNavigate()
  const search = useSearch({ from: '/suites/$suiteHash' }) as { tab?: string }
  const { data: suite, isLoading, error, refetch } = useSuite(suiteHash)

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
  const tabIndex = search.tab === 'warmup' && hasWarmup ? 1 : 0

  const handleTabChange = (index: number) => {
    const tab = index === 1 ? 'warmup' : 'tests'
    navigate({
      to: '/suites/$suiteHash',
      params: { suiteHash },
      search: { tab },
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

      <TabGroup selectedIndex={tabIndex} onChange={handleTabChange}>
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
