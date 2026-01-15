import { useState, useMemo } from 'react'
import clsx from 'clsx'
import type { TestEntry, SuiteFile } from '@/api/types'
import { Badge } from '@/components/shared/Badge'
import { Duration } from '@/components/shared/Duration'
import { Pagination } from '@/components/shared/Pagination'
import { MethodBreakdown } from './MethodBreakdown'

interface TestsTableProps {
  tests: Record<string, TestEntry>
  runId: string
  suiteTests?: SuiteFile[]
}

const PAGE_SIZE = 20

export function TestsTable({ tests, runId, suiteTests }: TestsTableProps) {
  const [currentPage, setCurrentPage] = useState(1)
  const [expandedTest, setExpandedTest] = useState<string | null>(null)
  const [searchQuery, setSearchQuery] = useState('')

  const executionOrder = useMemo(() => {
    if (!suiteTests) return new Map<string, number>()
    return new Map(suiteTests.map((file, index) => [file.f, index + 1]))
  }, [suiteTests])

  const sortedTests = useMemo(() => {
    let filtered = Object.entries(tests)

    if (searchQuery) {
      const query = searchQuery.toLowerCase()
      filtered = filtered.filter(([name]) => name.toLowerCase().includes(query))
    }

    if (executionOrder.size > 0) {
      return filtered.sort(([a], [b]) => {
        const orderA = executionOrder.get(a) ?? Infinity
        const orderB = executionOrder.get(b) ?? Infinity
        return orderA - orderB
      })
    }

    return filtered.sort(([a], [b]) => a.localeCompare(b))
  }, [tests, searchQuery, executionOrder])

  const totalPages = Math.ceil(sortedTests.length / PAGE_SIZE)
  const paginatedTests = sortedTests.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE)

  const toggleExpand = (testName: string) => {
    setExpandedTest(expandedTest === testName ? null : testName)
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <h2 className="text-lg/7 font-semibold text-gray-900 dark:text-gray-100">
          Tests ({sortedTests.length})
        </h2>
        <input
          type="text"
          placeholder="Search tests..."
          value={searchQuery}
          onChange={(e) => {
            setSearchQuery(e.target.value)
            setCurrentPage(1)
          }}
          className="w-64 rounded-sm border border-gray-300 bg-white px-3 py-2 text-sm/6 placeholder:text-gray-400 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100 dark:placeholder:text-gray-500"
        />
      </div>

      <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
        <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
          <thead className="bg-gray-50 dark:bg-gray-900">
            <tr>
              <th className="w-8 px-4 py-3"></th>
              <th className="w-12 px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                #
              </th>
              <th className="px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Test
              </th>
              <th className="px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Status
              </th>
              <th className="px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Total Time
              </th>
              <th className="px-4 py-3 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">
                Methods
              </th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {paginatedTests.map(([testName, entry]) => (
              <>
                <tr
                  key={testName}
                  onClick={() => toggleExpand(testName)}
                  className={clsx(
                    'cursor-pointer transition-colors hover:bg-gray-50 dark:hover:bg-gray-700/50',
                    expandedTest === testName && 'bg-blue-50 dark:bg-blue-900/20',
                  )}
                >
                  <td className="px-4 py-3">
                    <svg
                      className={clsx(
                        'size-4 text-gray-400 transition-transform',
                        expandedTest === testName && 'rotate-90',
                      )}
                      fill="none"
                      viewBox="0 0 24 24"
                      stroke="currentColor"
                    >
                      <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
                    </svg>
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-sm/6 font-medium text-gray-500 dark:text-gray-400">
                    {executionOrder.get(testName) ?? '-'}
                  </td>
                  <td className="max-w-md truncate px-4 py-3 text-sm/6 font-medium text-gray-900 dark:text-gray-100">
                    <span title={testName}>{testName}</span>
                  </td>
                  <td className="whitespace-nowrap px-4 py-3">
                    <div className="flex items-center gap-2">
                      {entry.aggregated.success > 0 && (
                        <Badge variant="success">{entry.aggregated.success}</Badge>
                      )}
                      {entry.aggregated.fail > 0 && <Badge variant="error">{entry.aggregated.fail}</Badge>}
                    </div>
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-sm/6 text-gray-500 dark:text-gray-400">
                    <Duration nanoseconds={entry.aggregated.time_total} />
                  </td>
                  <td className="whitespace-nowrap px-4 py-3 text-sm/6 text-gray-500 dark:text-gray-400">
                    {Object.keys(entry.aggregated.methods).length}
                  </td>
                </tr>
                {expandedTest === testName && (
                  <tr key={`${testName}-expanded`}>
                    <td colSpan={6} className="bg-gray-50 px-4 py-4 dark:bg-gray-900/50">
                      <MethodBreakdown
                        methods={entry.aggregated.methods}
                        runId={runId}
                        testName={testName}
                        dir={entry.dir}
                      />
                    </td>
                  </tr>
                )}
              </>
            ))}
          </tbody>
        </table>
      </div>

      {totalPages > 1 && (
        <div className="flex justify-center">
          <Pagination currentPage={currentPage} totalPages={totalPages} onPageChange={setCurrentPage} />
        </div>
      )}
    </div>
  )
}
