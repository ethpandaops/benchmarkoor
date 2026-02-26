import { useMemo, useState } from 'react'
import clsx from 'clsx'
import type { SuiteTest, AggregatedStats } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { Badge } from '@/components/shared/Badge'
import { Pagination } from '@/components/shared/Pagination'
import { type CompareRun, RUN_SLOTS } from './constants'

interface TestComparisonTableProps {
  runs: CompareRun[]
  suiteTests?: SuiteTest[]
  stepFilter: StepTypeOption[]
}

type SortColumn = 'order' | 'name' | 'deltaMgas'
type SortDirection = 'asc' | 'desc'

interface ComparedTest {
  name: string
  order: number
  mgas: (number | undefined)[]
  status: ('pass' | 'fail' | 'missing')[]
  deltaMgas: number | undefined
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

export function TestComparisonTable({ runs, suiteTests, stepFilter }: TestComparisonTableProps) {
  const [sortBy, setSortBy] = useState<SortColumn>('order')
  const [sortDir, setSortDir] = useState<SortDirection>('asc')
  const [searchQuery, setSearchQuery] = useState('')
  const [currentPage, setCurrentPage] = useState(1)

  const comparedTests = useMemo(() => {
    const allTestNames = new Set<string>()
    for (const run of runs) {
      if (run.result) {
        for (const name of Object.keys(run.result.tests)) {
          allTestNames.add(name)
        }
      }
    }

    const suiteOrder = new Map<string, number>()
    if (suiteTests) {
      suiteTests.forEach((t, i) => suiteOrder.set(t.name, i + 1))
    }

    const tests: ComparedTest[] = []
    for (const name of allTestNames) {
      const mgas: (number | undefined)[] = []
      const status: ('pass' | 'fail' | 'missing')[] = []
      let order = suiteOrder.get(name) ?? 0

      for (const run of runs) {
        const entry = run.result?.tests[name]
        const stats = entry ? getAggregatedStats(entry, stepFilter) : undefined
        mgas.push(calculateMGasPerSec(stats))

        if (stats) {
          status.push(stats.fail > 0 ? 'fail' : 'pass')
        } else {
          status.push('missing')
        }

        if (order === 0 && entry) {
          order = parseInt(entry.dir, 10) || 0
        }
      }

      const deltaMgas = mgas[0] !== undefined && mgas[mgas.length - 1] !== undefined
        ? mgas[mgas.length - 1]! - mgas[0]!
        : undefined

      tests.push({ name, order, mgas, status, deltaMgas })
    }
    return tests
  }, [runs, suiteTests, stepFilter])

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
      setSortDir('asc')
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
              {runs.map((run) => {
                const slot = RUN_SLOTS[run.index]
                return (
                  <th key={slot.label} className={clsx('px-4 py-3 text-right text-xs/5 font-medium uppercase tracking-wider', slot.textClass, `dark:${slot.textDarkClass.replace('text-', 'text-')}`)}>
                    <div className="flex flex-col items-end gap-1">
                      <img src={`/img/clients/${run.config.instance.client}.jpg`} alt={run.config.instance.client} className="size-5 rounded-full object-cover" />
                      {slot.label} MGas/s
                    </div>
                  </th>
                )
              })}
              <SortableHeader label={'\u0394 MGas/s'} column="deltaMgas" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="px-4 py-3 text-right" />
              <th className="px-3 py-3 text-center text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">Status</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {paginatedTests.map((test) => {
              const deltaMgasColor = test.deltaMgas !== undefined && test.deltaMgas !== 0
                ? test.deltaMgas > 0 ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'
                : 'text-gray-400 dark:text-gray-500'

              return (
                <tr key={test.name} className="hover:bg-gray-50 dark:hover:bg-gray-700/50">
                  <td className="whitespace-nowrap px-3 py-2 text-center text-xs/5 text-gray-400 dark:text-gray-500">
                    {test.order || '-'}
                  </td>
                  <td className="max-w-sm truncate px-4 py-2 text-sm/6 text-gray-900 dark:text-gray-100" title={test.name}>
                    {test.name}
                  </td>
                  {test.mgas.map((val, i) => (
                    <td key={RUN_SLOTS[i].label} className="whitespace-nowrap px-4 py-2 text-right text-sm/6 text-gray-500 dark:text-gray-400">
                      {val !== undefined ? val.toFixed(2) : '-'}
                    </td>
                  ))}
                  <td className={clsx('whitespace-nowrap px-4 py-2 text-right text-sm/6 font-medium', deltaMgasColor)}>
                    {test.deltaMgas !== undefined ? `${test.deltaMgas > 0 ? '+' : ''}${test.deltaMgas.toFixed(2)}` : '-'}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2 text-center">
                    <div className="flex items-center justify-center gap-1">
                      {test.status.map((s, i) => (
                        <StatusDot key={RUN_SLOTS[i].label} status={s} label={RUN_SLOTS[i].label} />
                      ))}
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
