import { useMemo, useState } from 'react'
import clsx from 'clsx'
import type { RunResult, SuiteTest, AggregatedStats } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { Duration } from '@/components/shared/Duration'
import { Badge } from '@/components/shared/Badge'
import { Pagination } from '@/components/shared/Pagination'

interface TestComparisonTableProps {
  resultA: RunResult
  resultB: RunResult
  suiteTests?: SuiteTest[]
  stepFilter: StepTypeOption[]
}

type SortColumn = 'order' | 'name' | 'deltaMgas' | 'deltaTime'
type SortDirection = 'asc' | 'desc'

interface ComparedTest {
  name: string
  order: number
  statsA: AggregatedStats | undefined
  statsB: AggregatedStats | undefined
  mgasA: number | undefined
  mgasB: number | undefined
  timeA: number
  timeB: number
  statusA: 'pass' | 'fail' | 'missing'
  statusB: 'pass' | 'fail' | 'missing'
  deltaMgas: number | undefined
  deltaTime: number
}

function calculateMGasPerSec(stats: AggregatedStats | undefined): number | undefined {
  if (!stats || stats.gas_used_time_total <= 0 || stats.gas_used_total <= 0) return undefined
  return (stats.gas_used_total * 1000) / stats.gas_used_time_total
}

function SortIcon({ direction, active }: { direction: SortDirection; active: boolean }) {
  return (
    <svg
      className={clsx('ml-1 inline-block size-3', active ? 'text-gray-700 dark:text-gray-300' : 'text-gray-400')}
      viewBox="0 0 12 12"
      fill="currentColor"
    >
      {direction === 'asc' ? <path d="M6 2L10 8H2L6 2Z" /> : <path d="M6 10L2 4H10L6 10Z" />}
    </svg>
  )
}

function SortableHeader({
  label,
  column,
  className,
  currentSort,
  currentDirection,
  onSort,
}: {
  label: string
  column: SortColumn
  className?: string
  currentSort: SortColumn
  currentDirection: SortDirection
  onSort: (column: SortColumn) => void
}) {
  const isActive = currentSort === column
  return (
    <th
      onClick={() => onSort(column)}
      className={clsx(
        'cursor-pointer select-none text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-300',
        className ?? 'px-4 py-3',
      )}
    >
      {label}
      <SortIcon direction={isActive ? currentDirection : 'asc'} active={isActive} />
    </th>
  )
}

const PAGE_SIZE = 50

export function TestComparisonTable({ resultA, resultB, suiteTests, stepFilter }: TestComparisonTableProps) {
  const [sortBy, setSortBy] = useState<SortColumn>('order')
  const [sortDir, setSortDir] = useState<SortDirection>('asc')
  const [searchQuery, setSearchQuery] = useState('')
  const [currentPage, setCurrentPage] = useState(1)

  const comparedTests = useMemo(() => {
    const allTestNames = new Set([...Object.keys(resultA.tests), ...Object.keys(resultB.tests)])
    const suiteOrder = new Map<string, number>()
    if (suiteTests) {
      suiteTests.forEach((t, i) => suiteOrder.set(t.name, i + 1))
    }

    const tests: ComparedTest[] = []
    for (const name of allTestNames) {
      const entryA = resultA.tests[name]
      const entryB = resultB.tests[name]
      const statsA = entryA ? getAggregatedStats(entryA, stepFilter) : undefined
      const statsB = entryB ? getAggregatedStats(entryB, stepFilter) : undefined
      const mgasA = calculateMGasPerSec(statsA)
      const mgasB = calculateMGasPerSec(statsB)
      const timeA = statsA?.time_total ?? 0
      const timeB = statsB?.time_total ?? 0

      let statusA: 'pass' | 'fail' | 'missing' = 'missing'
      if (statsA) statusA = statsA.fail > 0 ? 'fail' : 'pass'
      let statusB: 'pass' | 'fail' | 'missing' = 'missing'
      if (statsB) statusB = statsB.fail > 0 ? 'fail' : 'pass'

      const order = suiteOrder.get(name) ?? (entryA ? parseInt(entryA.dir, 10) || 0 : (entryB ? parseInt(entryB.dir, 10) || 0 : 0))

      tests.push({
        name,
        order,
        statsA,
        statsB,
        mgasA,
        mgasB,
        timeA,
        timeB,
        statusA,
        statusB,
        deltaMgas: mgasA !== undefined && mgasB !== undefined ? mgasB - mgasA : undefined,
        deltaTime: timeB - timeA,
      })
    }
    return tests
  }, [resultA, resultB, suiteTests, stepFilter])

  const filteredTests = useMemo(() => {
    if (!searchQuery) return comparedTests
    const q = searchQuery.toLowerCase()
    return comparedTests.filter((t) => t.name.toLowerCase().includes(q))
  }, [comparedTests, searchQuery])

  const sortedTests = useMemo(() => {
    const sorted = [...filteredTests]
    sorted.sort((a, b) => {
      let cmp = 0
      switch (sortBy) {
        case 'order':
          cmp = a.order - b.order
          break
        case 'name':
          cmp = a.name.localeCompare(b.name)
          break
        case 'deltaMgas':
          cmp = (a.deltaMgas ?? 0) - (b.deltaMgas ?? 0)
          break
        case 'deltaTime':
          cmp = a.deltaTime - b.deltaTime
          break
      }
      return sortDir === 'asc' ? cmp : -cmp
    })
    return sorted
  }, [filteredTests, sortBy, sortDir])

  const totalPages = Math.ceil(sortedTests.length / PAGE_SIZE)
  const paginatedTests = sortedTests.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE)

  const handleSort = (column: SortColumn) => {
    if (sortBy === column) {
      setSortDir(sortDir === 'desc' ? 'asc' : 'desc')
    } else {
      setSortBy(column)
      setSortDir(column === 'deltaMgas' ? 'asc' : 'asc')
    }
    setCurrentPage(1)
  }

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <div className="flex items-center justify-between border-b border-gray-200 px-4 py-3 dark:border-gray-700">
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">
          Per-Test Comparison ({filteredTests.length})
        </h3>
        <input
          type="text"
          placeholder="Filter tests..."
          value={searchQuery}
          onChange={(e) => { setSearchQuery(e.target.value); setCurrentPage(1) }}
          className="rounded-xs border border-gray-300 bg-white px-3 py-1 text-sm/6 placeholder-gray-400 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 dark:placeholder-gray-500"
        />
      </div>
      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
          <thead className="bg-gray-50 dark:bg-gray-900">
            <tr>
              <SortableHeader label="#" column="order" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="w-12 px-3 py-3" />
              <SortableHeader label="Test Name" column="name" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
              <th className="px-4 py-3 text-right text-xs/5 font-medium uppercase tracking-wider text-blue-600 dark:text-blue-400">A MGas/s</th>
              <th className="px-4 py-3 text-right text-xs/5 font-medium uppercase tracking-wider text-amber-600 dark:text-amber-400">B MGas/s</th>
              <SortableHeader label={'\u0394 MGas/s'} column="deltaMgas" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="px-4 py-3 text-right" />
              <th className="px-4 py-3 text-right text-xs/5 font-medium uppercase tracking-wider text-blue-600 dark:text-blue-400">A Time</th>
              <th className="px-4 py-3 text-right text-xs/5 font-medium uppercase tracking-wider text-amber-600 dark:text-amber-400">B Time</th>
              <SortableHeader label={'\u0394 Time'} column="deltaTime" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="px-4 py-3 text-right" />
              <th className="px-3 py-3 text-center text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">Status</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {paginatedTests.map((test) => {
              const deltaMgasColor = test.deltaMgas !== undefined && test.deltaMgas !== 0
                ? test.deltaMgas > 0 ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'
                : 'text-gray-400 dark:text-gray-500'
              const deltaTimeColor = test.deltaTime !== 0
                ? test.deltaTime < 0 ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'
                : 'text-gray-400 dark:text-gray-500'

              return (
                <tr key={test.name} className="hover:bg-gray-50 dark:hover:bg-gray-700/50">
                  <td className="whitespace-nowrap px-3 py-2 text-center text-xs/5 text-gray-400 dark:text-gray-500">
                    {test.order || '-'}
                  </td>
                  <td className="max-w-sm truncate px-4 py-2 text-sm/6 text-gray-900 dark:text-gray-100" title={test.name}>
                    {test.name}
                  </td>
                  <td className="whitespace-nowrap px-4 py-2 text-right text-sm/6 text-gray-500 dark:text-gray-400">
                    {test.mgasA !== undefined ? test.mgasA.toFixed(2) : '-'}
                  </td>
                  <td className="whitespace-nowrap px-4 py-2 text-right text-sm/6 text-gray-500 dark:text-gray-400">
                    {test.mgasB !== undefined ? test.mgasB.toFixed(2) : '-'}
                  </td>
                  <td className={clsx('whitespace-nowrap px-4 py-2 text-right text-sm/6 font-medium', deltaMgasColor)}>
                    {test.deltaMgas !== undefined ? `${test.deltaMgas > 0 ? '+' : ''}${test.deltaMgas.toFixed(2)}` : '-'}
                  </td>
                  <td className="whitespace-nowrap px-4 py-2 text-right text-sm/6 text-gray-500 dark:text-gray-400">
                    {test.timeA > 0 ? <Duration nanoseconds={test.timeA} /> : '-'}
                  </td>
                  <td className="whitespace-nowrap px-4 py-2 text-right text-sm/6 text-gray-500 dark:text-gray-400">
                    {test.timeB > 0 ? <Duration nanoseconds={test.timeB} /> : '-'}
                  </td>
                  <td className={clsx('whitespace-nowrap px-4 py-2 text-right text-sm/6 font-medium', deltaTimeColor)}>
                    {test.timeA > 0 && test.timeB > 0 ? (
                      <>{test.deltaTime > 0 ? '+' : ''}<Duration nanoseconds={Math.abs(test.deltaTime)} /></>
                    ) : '-'}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-center">
                    <div className="flex items-center justify-center gap-1">
                      <StatusDot status={test.statusA} label="A" />
                      <StatusDot status={test.statusB} label="B" />
                    </div>
                  </td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
      {totalPages > 1 && (
        <div className="flex justify-end border-t border-gray-200 px-4 py-3 dark:border-gray-700">
          <Pagination currentPage={currentPage} totalPages={totalPages} onPageChange={setCurrentPage} />
        </div>
      )}
    </div>
  )
}

function StatusDot({ status, label }: { status: 'pass' | 'fail' | 'missing'; label: string }) {
  if (status === 'missing') {
    return <Badge variant="default">{label}:-</Badge>
  }
  return (
    <Badge variant={status === 'pass' ? 'success' : 'error'}>
      {label}
    </Badge>
  )
}
