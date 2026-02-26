import { useMemo, useState } from 'react'
import clsx from 'clsx'
import type { SuiteTest, AggregatedStats } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { Pagination } from '@/components/shared/Pagination'
import { type CompareRun, RUN_SLOTS } from './constants'

interface TestComparisonTableProps {
  runs: CompareRun[]
  suiteTests?: SuiteTest[]
  stepFilter: StepTypeOption[]
}

type SortColumn = 'order' | 'name' | 'avgMgas'
type SortDirection = 'asc' | 'desc'

interface ComparedTest {
  name: string
  order: number
  mgas: (number | undefined)[]
  avgMgas: number | undefined
}

// Returns an RGB color interpolated from yellow (small diff) to red (large diff)
// based on the percentage deviation from the best value.
function getDiffColor(diff: number, best: number): string {
  if (best <= 0) return 'rgb(239, 68, 68)' // red
  const pct = Math.abs(diff) / best // 0..1+
  const t = Math.min(pct / 0.5, 1) // clamp: 0% → 0, ≥50% → 1
  // yellow (234,179,8) → red (239,68,68)
  const r = Math.round(234 + t * (239 - 234))
  const g = Math.round(179 - t * (179 - 68))
  const b = Math.round(8 + t * (68 - 8))
  return `rgb(${r}, ${g}, ${b})`
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
  const [useRegex, setUseRegex] = useState(false)
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
      let order = suiteOrder.get(name) ?? 0

      for (const run of runs) {
        const entry = run.result?.tests[name]
        const stats = entry ? getAggregatedStats(entry, stepFilter) : undefined
        mgas.push(calculateMGasPerSec(stats))

        if (order === 0 && entry) {
          order = parseInt(entry.dir, 10) || 0
        }
      }

      const defined = mgas.filter((v): v is number => v !== undefined)
      const avgMgas = defined.length > 0 ? defined.reduce((a, b) => a + b, 0) / defined.length : undefined

      tests.push({ name, order, mgas, avgMgas })
    }
    return tests
  }, [runs, suiteTests, stepFilter])

  const filteredTests = useMemo(() => {
    if (!searchQuery) return comparedTests
    if (useRegex) {
      try {
        const re = new RegExp(searchQuery, 'i')
        return comparedTests.filter((t) => re.test(t.name))
      } catch {
        return comparedTests
      }
    }
    const q = searchQuery.toLowerCase()
    return comparedTests.filter((t) => t.name.toLowerCase().includes(q))
  }, [comparedTests, searchQuery, useRegex])

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
        case 'avgMgas':
          cmp = (a.avgMgas ?? 0) - (b.avgMgas ?? 0)
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
        <div className="flex items-center gap-1.5">
          <input
            type="text"
            placeholder={useRegex ? 'Regex pattern...' : 'Filter tests...'}
            value={searchQuery}
            onChange={(e) => { setSearchQuery(e.target.value); setCurrentPage(1) }}
            className={clsx(
              'rounded-xs border bg-white px-3 py-1 text-sm/6 placeholder-gray-400 focus:outline-hidden focus:ring-1 dark:bg-gray-700 dark:text-gray-100 dark:placeholder-gray-500',
              useRegex && searchQuery && (() => { try { new RegExp(searchQuery); return false } catch { return true } })()
                ? 'border-red-400 focus:border-red-500 focus:ring-red-500 dark:border-red-500'
                : 'border-gray-300 focus:border-blue-500 focus:ring-blue-500 dark:border-gray-600',
            )}
          />
          <button
            onClick={() => setUseRegex(!useRegex)}
            title={useRegex ? 'Regex mode (click to switch to text)' : 'Text mode (click to switch to regex)'}
            className={clsx(
              'rounded-xs px-1.5 py-1 font-mono text-sm/6 transition-colors',
              useRegex
                ? 'bg-blue-500 text-white'
                : 'border border-gray-300 bg-white text-gray-500 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
            )}
          >
            .*
          </button>
        </div>
      </div>
      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
          <thead className="bg-gray-50 dark:bg-gray-900">
            <tr>
              <SortableHeader label="#" column="order" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="w-12 px-3 py-3" />
              <SortableHeader label="Test Name" column="name" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
              <SortableHeader label="Avg" column="avgMgas" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="px-4 py-3 text-right" />
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
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {paginatedTests.map((test) => {
              const definedMgas = test.mgas.filter((v): v is number => v !== undefined)
              const maxMgas = definedMgas.length > 0 ? Math.max(...definedMgas) : undefined

              return (
                <tr key={test.name} className="hover:bg-gray-50 dark:hover:bg-gray-700/50">
                  <td className="whitespace-nowrap px-3 py-2 text-center text-xs/5 text-gray-400 dark:text-gray-500">
                    {test.order || '-'}
                  </td>
                  <td className="max-w-sm truncate px-4 py-2 text-sm/6 text-gray-900 dark:text-gray-100" title={test.name}>
                    {test.name}
                  </td>
                  <td className="whitespace-nowrap px-4 py-2 text-right text-sm/6 text-gray-400 dark:text-gray-500">
                    {test.avgMgas !== undefined ? test.avgMgas.toFixed(2) : '-'}
                  </td>
                  {test.mgas.map((val, i) => {
                    const diff = val !== undefined && maxMgas !== undefined ? val - maxMgas : undefined
                    const isFastest = val !== undefined && val === maxMgas
                    return (
                      <td key={RUN_SLOTS[i].label} className="whitespace-nowrap px-4 py-2 text-right text-sm/6">
                        <div className={isFastest ? 'font-semibold text-gray-900 dark:text-gray-100' : 'text-gray-500 dark:text-gray-400'}>
                          {val !== undefined ? val.toFixed(2) : '-'}
                        </div>
                        {diff !== undefined && !isFastest && (
                          <div className="text-xs/4" style={{ color: getDiffColor(diff, maxMgas!) }}>
                            {diff.toFixed(2)}
                          </div>
                        )}
                      </td>
                    )
                  })}
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
