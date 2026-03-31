import { useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import clsx from 'clsx'
import { Table } from 'lucide-react'
import type { SuiteTest, AggregatedStats, BlockLogs, BlockLogEntry } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { Pagination } from '@/components/shared/Pagination'
import { type CompareRun, type LabelMode, RUN_SLOTS, formatRunLabel } from './constants'

interface TestComparisonTableProps {
  runs: CompareRun[]
  suiteTests?: SuiteTest[]
  stepFilter: StepTypeOption[]
  blockLogsPerRun?: (BlockLogs | null)[]
  labelMode: LabelMode
  tableBaseline: TableBaseline
  onTableBaselineChange: (val: TableBaseline) => void
  sortBy: SortColumn
  sortDir: SortDirection
  onSortChange: (column: SortColumn, direction: SortDirection) => void
  testNameFilter?: (name: string) => boolean
}

type SortColumn = 'order' | 'name' | 'avgValue' | `run-${number}`
type SortDirection = 'asc' | 'desc'
type TableBaseline = 'best' | 'worst' | number

interface ComparedTest {
  name: string
  order: number
  values: (number | undefined)[]
  avgValue: number | undefined
}

interface MetricTab {
  id: string
  label: string
  unit: string
  higherIsBetter: boolean
  format: (v: number) => string
}

const BLOCK_LOG_METRICS: MetricTab[] = [
  { id: 'bl-throughput', label: 'Throughput', unit: 'MGas/s', higherIsBetter: true, format: (v) => v >= 100 ? v.toFixed(0) : v.toFixed(2) },
  { id: 'bl-execution', label: 'Execution Time', unit: 'ms', higherIsBetter: false, format: (v) => v.toFixed(2) },
  { id: 'bl-overhead', label: 'Overhead', unit: 'ms', higherIsBetter: false, format: (v) => v.toFixed(2) },
  { id: 'bl-account-cache', label: 'Account Cache HR', unit: '%', higherIsBetter: true, format: (v) => v.toFixed(1) },
  { id: 'bl-storage-cache', label: 'Storage Cache HR', unit: '%', higherIsBetter: true, format: (v) => v.toFixed(1) },
  { id: 'bl-code-cache', label: 'Code Cache HR', unit: '%', higherIsBetter: true, format: (v) => v.toFixed(1) },
]

function extractBlockLogMetric(entry: BlockLogEntry, metricId: string): number {
  switch (metricId) {
    case 'bl-throughput': return entry.throughput?.mgas_per_sec ?? 0
    case 'bl-execution': return entry.timing?.execution_ms ?? 0
    case 'bl-overhead': return (entry.timing?.state_read_ms ?? 0) + (entry.timing?.state_hash_ms ?? 0) + (entry.timing?.commit_ms ?? 0)
    case 'bl-account-cache': return entry.cache?.account?.hit_rate ?? 0
    case 'bl-storage-cache': return entry.cache?.storage?.hit_rate ?? 0
    case 'bl-code-cache': return entry.cache?.code?.hit_rate ?? 0
    default: return 0
  }
}

// Returns an RGB color based on percentage deviation from reference value.
// Positive diff (better): green tones. Negative diff (worse): yellow → red.
function getDiffColor(diff: number, ref: number): string {
  if (ref <= 0) return 'rgb(239, 68, 68)'
  const pct = Math.abs(diff) / ref
  const t = Math.min(pct / 0.5, 1)
  if (diff >= 0) {
    // light green (134,239,172) → green (22,163,74)
    const r = Math.round(134 - t * (134 - 22))
    const g = Math.round(239 - t * (239 - 163))
    const b = Math.round(172 - t * (172 - 74))
    return `rgb(${r}, ${g}, ${b})`
  }
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

export function TestComparisonTable({ runs, suiteTests, stepFilter, blockLogsPerRun, labelMode, tableBaseline, onTableBaselineChange, sortBy, sortDir, onSortChange, testNameFilter }: TestComparisonTableProps) {
  const [activeTab, setActiveTab] = useState('mgas')
  const [currentPage, setCurrentPage] = useState(1)

  const hasBlockLogs = blockLogsPerRun?.some((bl) => bl !== null) ?? false

  const tabs: MetricTab[] = useMemo(() => {
    const base: MetricTab[] = [
      { id: 'mgas', label: 'MGas/s', unit: 'MGas/s', higherIsBetter: true, format: (v) => v >= 100 ? v.toFixed(0) : v.toFixed(2) },
    ]
    if (hasBlockLogs) {
      return [...base, ...BLOCK_LOG_METRICS]
    }
    return base
  }, [hasBlockLogs])

  const activeMetric = tabs.find((t) => t.id === activeTab) ?? tabs[0]

  const suiteOrder = useMemo(() => {
    const map = new Map<string, number>()
    if (suiteTests) {
      suiteTests.forEach((t, i) => map.set(t.name, i + 1))
    }
    return map
  }, [suiteTests])

  const comparedTests = useMemo(() => {
    const allTestNames = new Set<string>()

    if (activeTab === 'mgas') {
      for (const run of runs) {
        if (run.result) {
          for (const name of Object.keys(run.result.tests)) allTestNames.add(name)
        }
      }
    } else {
      if (blockLogsPerRun) {
        for (const bl of blockLogsPerRun) {
          if (bl) for (const name of Object.keys(bl)) allTestNames.add(name)
        }
      }
    }

    const tests: ComparedTest[] = []
    for (const name of allTestNames) {
      const values: (number | undefined)[] = []
      let order = suiteOrder.get(name) ?? 0

      if (activeTab === 'mgas') {
        for (const run of runs) {
          const entry = run.result?.tests[name]
          const stats = entry ? getAggregatedStats(entry, stepFilter) : undefined
          values.push(calculateMGasPerSec(stats))
          if (order === 0 && entry) {
            order = parseInt(entry.dir, 10) || 0
          }
        }
      } else {
        for (let i = 0; i < runs.length; i++) {
          const bl = blockLogsPerRun?.[i]
          const entry = bl?.[name]
          values.push(entry ? extractBlockLogMetric(entry, activeTab) : undefined)
        }
      }

      const defined = values.filter((v): v is number => v !== undefined)
      const avgValue = defined.length > 0 ? defined.reduce((a, b) => a + b, 0) / defined.length : undefined

      tests.push({ name, order, values, avgValue })
    }
    return tests
  }, [runs, suiteOrder, stepFilter, activeTab, blockLogsPerRun])

  const filteredTests = useMemo(() => {
    if (!testNameFilter) return comparedTests
    return comparedTests.filter((t) => testNameFilter(t.name))
  }, [comparedTests, testNameFilter])

  const sortedTests = useMemo(() => {
    const sorted = [...filteredTests]
    sorted.sort((a, b) => {
      let cmp = 0
      if (sortBy.startsWith('run-') && typeof tableBaseline === 'number') {
        const runIdx = parseInt(sortBy.slice(4), 10)
        const getPctDiff = (test: ComparedTest) => {
          const val = test.values[runIdx]
          const ref = test.values[tableBaseline]
          if (val === undefined || ref === undefined || ref === 0) return undefined
          return ((val - ref) / ref) * 100
        }
        const aDiff = getPctDiff(a)
        const bDiff = getPctDiff(b)
        if (aDiff === undefined && bDiff === undefined) cmp = 0
        else if (aDiff === undefined) cmp = 1
        else if (bDiff === undefined) cmp = -1
        else cmp = aDiff - bDiff
      } else {
        switch (sortBy) {
          case 'order':
            cmp = a.order - b.order
            break
          case 'name':
            cmp = a.name.localeCompare(b.name)
            break
          case 'avgValue':
            cmp = (a.avgValue ?? 0) - (b.avgValue ?? 0)
            break
        }
      }
      return sortDir === 'asc' ? cmp : -cmp
    })
    return sorted
  }, [filteredTests, sortBy, sortDir, tableBaseline])

  const totalPages = Math.ceil(sortedTests.length / PAGE_SIZE)
  const paginatedTests = sortedTests.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE)

  const handleSort = (column: SortColumn) => {
    if (sortBy === column) {
      onSortChange(column, sortDir === 'desc' ? 'asc' : 'desc')
    } else {
      onSortChange(column, 'asc')
    }
    setCurrentPage(1)
  }

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <div className="flex items-center border-b border-gray-200 px-4 py-3 dark:border-gray-700">
        <div className="flex items-center gap-2">
          <Table className="size-4 text-gray-400 dark:text-gray-500" />
          <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Per-Test Comparison ({filteredTests.length})</h3>
        </div>
      </div>

      {tabs.length > 1 && (
        <div className="flex gap-0 overflow-x-auto border-b border-gray-200 px-4 dark:border-gray-700">
          {tabs.map((tab) => (
            <button
              key={tab.id}
              onClick={() => { setActiveTab(tab.id); setCurrentPage(1) }}
              title={tab.id.startsWith('bl-') ? 'Extracted from block logs' : undefined}
              className={clsx(
                'shrink-0 border-b-2 px-3 py-2 text-xs/5 font-medium transition-colors',
                activeTab === tab.id
                  ? 'border-blue-500 text-blue-600 dark:border-blue-400 dark:text-blue-400'
                  : 'border-transparent text-gray-500 hover:border-gray-300 hover:text-gray-700 dark:text-gray-400 dark:hover:border-gray-600 dark:hover:text-gray-300',
              )}
            >
              {tab.label}
            </button>
          ))}
        </div>
      )}

      <div className="flex items-center gap-1.5 border-b border-gray-200 px-4 py-2 dark:border-gray-700">
        <span className="text-xs/5 text-gray-500 dark:text-gray-400">Baseline:</span>
        <div className="flex flex-wrap gap-1">
          {(['best', 'worst'] as const).map((mode) => (
            <button
              key={mode}
              onClick={() => onTableBaselineChange(mode)}
              className={clsx(
                'rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                tableBaseline === mode
                  ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                  : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
              )}
            >
              {mode === 'best' ? (activeMetric.higherIsBetter ? 'Fastest' : 'Lowest') : (activeMetric.higherIsBetter ? 'Slowest' : 'Highest')}
            </button>
          ))}
          {runs.map((run, i) => {
            const slot = RUN_SLOTS[run.index]
            return (
              <button
                key={slot.label}
                onClick={() => onTableBaselineChange(i)}
                className={clsx(
                  'inline-flex items-center gap-1 rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                  tableBaseline === i
                    ? `${slot.badgeBgClass} ${slot.badgeTextClass} ring-1 ring-current`
                    : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
                )}
              >
                <img src={`/img/clients/${run.config.instance.client}.jpg`} alt={run.config.instance.client} className="size-3.5 rounded-full object-cover" />
                {formatRunLabel(slot, run, labelMode)}
              </button>
            )
          })}
        </div>
      </div>

      <div className="overflow-x-auto">
        <table className="min-w-full divide-y divide-gray-200 dark:divide-gray-700">
          <thead className="bg-gray-50 dark:bg-gray-900">
            <tr>
              <SortableHeader label="#" column="order" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="w-12 px-3 py-3" />
              <SortableHeader label="Test Name" column="name" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} />
              <SortableHeader label="Avg" column="avgValue" currentSort={sortBy} currentDirection={sortDir} onSort={handleSort} className="px-4 py-3 text-right" />
              {runs.map((run, i) => {
                const slot = RUN_SLOTS[run.index]
                const colId: SortColumn = `run-${i}`
                const isSortable = typeof tableBaseline === 'number' && i !== tableBaseline
                const isActive = sortBy === colId
                return (
                  <th
                    key={slot.label}
                    onClick={isSortable ? () => handleSort(colId) : undefined}
                    className={clsx(
                      'px-4 py-3 text-right text-xs/5 font-medium uppercase tracking-wider',
                      slot.textClass, `dark:${slot.textDarkClass.replace('text-', 'text-')}`,
                      isSortable && 'cursor-pointer select-none',
                    )}
                  >
                    <div className="flex flex-col items-end gap-1" title={formatRunLabel(slot, run, labelMode)}>
                      <img src={`/img/clients/${run.config.instance.client}.jpg`} alt={run.config.instance.client} className="size-5 rounded-full object-cover" />
                      <span className="inline-flex items-center">
                        {slot.label}
                        {isSortable && <SortIcon direction={isActive ? sortDir : 'asc'} active={isActive} />}
                      </span>
                      <span>{activeMetric.unit}</span>
                    </div>
                  </th>
                )
              })}
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
            {paginatedTests.map((test) => {
              const defined = test.values.filter((v): v is number => v !== undefined)
              let refValue: number | undefined
              if (typeof tableBaseline === 'number') {
                refValue = test.values[tableBaseline]
              } else if (defined.length > 0) {
                refValue = tableBaseline === 'best'
                  ? (activeMetric.higherIsBetter ? Math.max(...defined) : Math.min(...defined))
                  : (activeMetric.higherIsBetter ? Math.min(...defined) : Math.max(...defined))
              }

              return (
                <tr key={test.name} className="hover:bg-gray-50 dark:hover:bg-gray-700/50">
                  <td className="whitespace-nowrap px-3 py-2 text-center text-xs/5 text-gray-400 dark:text-gray-500">
                    {test.order || '-'}
                  </td>
                  <td className="max-w-sm truncate px-4 py-2 text-sm/6 text-gray-900 dark:text-gray-100" title={test.name}>
                    {test.name}
                  </td>
                  <td className="whitespace-nowrap px-4 py-2 text-right text-sm/6 text-gray-400 dark:text-gray-500">
                    {test.avgValue !== undefined ? activeMetric.format(test.avgValue) : '-'}
                  </td>
                  {test.values.map((val, i) => {
                    const isRef = val !== undefined && val === refValue
                    const diff = val !== undefined && refValue !== undefined
                      ? (activeMetric.higherIsBetter ? val - refValue : refValue - val)
                      : undefined
                    return (
                      <td key={RUN_SLOTS[i].label} className="whitespace-nowrap px-4 py-2 text-right text-sm/6">
                        {val !== undefined ? (
                          <Link
                            to="/runs/$runId"
                            params={{ runId: runs[i].runId }}
                            search={{ testModal: test.name }}
                            target="_blank"
                            className="block hover:underline"
                          >
                            <div className={isRef ? 'font-semibold text-gray-900 dark:text-gray-100' : 'text-gray-500 dark:text-gray-400'}>
                              {activeMetric.format(val)}
                            </div>
                            {diff !== undefined && !isRef && refValue! > 0 && (
                              <div className="text-xs/4" style={{ color: getDiffColor(diff, refValue!) }}>
                                {diff >= 0 ? '+' : '-'}{activeMetric.format(Math.abs(diff))}
                                {' '}({diff >= 0 ? '+' : '-'}{((Math.abs(diff) / refValue!) * 100).toFixed(1)}%)
                              </div>
                            )}
                          </Link>
                        ) : (
                          <div className="text-gray-500 dark:text-gray-400">-</div>
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
