import { useMemo, useState } from 'react'
import clsx from 'clsx'
import { BarChart3, ChevronDown, ChevronUp, Table2, X } from 'lucide-react'
import type { TestEntry } from '@/api/types'
import { parseEESTName } from '@/utils/eestName'
import { compileQuery, queryTermDimension, searchQueryContains, splitQuery } from '@/utils/eestNameFilter'
import { type StepTypeOption, ALL_STEP_TYPES, getAggregatedStats } from '@/pages/RunDetailPage'
import { percentile } from './block-logs-dashboard/utils/statistics'
import { DEFAULT_THRESHOLD, getColorByThreshold } from '@/utils/perfThreshold'

interface DimensionInsightsProps {
  tests: Record<string, TestEntry>
  stepFilter: StepTypeOption[]
  searchQuery?: string
  statusFilter?: 'all' | 'passed' | 'failed'
  /** Toggle a `key=value` term in the page-level search. */
  onToggle: (term: string) => void
  /** Current search query, used to highlight bars whose value is pinned. */
  query: string
  /** Slow-threshold MGas/s (shared with the Performance Heatmap). */
  threshold?: number
  /**
   * Open the test detail modal for a specific test. When provided and the
   * user clicks a not-yet-active value with only one matching test, the
   * modal opens for that test instead of adding a redundant filter.
   */
  onTestClick?: (testName: string) => void
}

type DimensionDef = { key: string; label: string; emitKey: string }

const PRIMARY_DIMENSIONS: DimensionDef[] = [
  { key: 'file', label: 'File', emitKey: 'file' },
  { key: 'fn', label: 'Test', emitKey: 'fn' },
  { key: 'benchmark', label: 'Gas', emitKey: 'gas' },
  { key: 'opcode', label: 'Opcode', emitKey: 'opcode' },
  { key: 'fork', label: 'Fork', emitKey: 'fork' },
]

const TRAILING_DIMENSIONS: DimensionDef[] = [
  { key: 'label', label: 'Label', emitKey: 'label' },
]

// Dimensions visible by default in the Bars view. Anything with an active
// filter is also forced into the preview so users always see the dim
// they're filtering by.
const BARS_PREVIEW_KEYS = new Set(['file', 'benchmark'])

interface ValueAgg {
  value: string
  count: number
  mean: number
  p50: number
  p95: number
  p99: number
  /** When count === 1, the single test's name. Used for "click to open". */
  singleTestName?: string
}

interface DimensionAgg {
  def: DimensionDef
  values: ValueAgg[]
}

function calculateMGasPerSec(gas: number, timeNs: number): number | undefined {
  if (timeNs <= 0 || gas <= 0) return undefined
  return (gas * 1000) / timeNs
}

function compareNumeric(a: string, b: string): number {
  const na = parseInt(a, 10)
  const nb = parseInt(b, 10)
  const aIsNum = !Number.isNaN(na) && /^\d/.test(a)
  const bIsNum = !Number.isNaN(nb) && /^\d/.test(b)
  if (aIsNum && bIsNum) return na - nb
  return a.localeCompare(b)
}

type SortColumn = 'value' | 'count' | 'mean' | 'p50' | 'p95' | 'p99'

export function DimensionInsights({
  tests,
  stepFilter,
  searchQuery,
  statusFilter = 'all',
  onToggle,
  query,
  threshold = DEFAULT_THRESHOLD,
  onTestClick,
}: DimensionInsightsProps) {
  const [view, setView] = useState<'bars' | 'table'>('bars')
  const [barsDir, setBarsDir] = useState<'desc' | 'asc'>('asc')
  const [showAllBars, setShowAllBars] = useState(false)
  const [groupByKey, setGroupByKey] = useState<string | null>(null)
  const [tableSort, setTableSort] = useState<{ col: SortColumn; dir: 'asc' | 'desc' }>({ col: 'mean', dir: 'desc' })

  const dimensions = useMemo<DimensionAgg[]>(() => {
    // 1. Compute mgas per test, applying the same filter as the heatmap.
    const matchesQuery = searchQuery ? compileQuery(searchQuery) : null
    const samples: { name: string; mgas: number }[] = []
    for (const [name, entry] of Object.entries(tests)) {
      if (matchesQuery && !matchesQuery(name)) continue
      const stats = getAggregatedStats(entry, stepFilter)
      if (!stats) continue
      if (statusFilter !== 'all') {
        const all = getAggregatedStats(entry, ALL_STEP_TYPES)
        if (!all) continue
        if (statusFilter === 'passed' && all.fail !== 0) continue
        if (statusFilter === 'failed' && all.fail === 0) continue
      }
      const mgas = calculateMGasPerSec(stats.gas_used_total, stats.gas_used_time_total)
      if (mgas === undefined) continue
      samples.push({ name, mgas })
    }

    // 2. Bucket samples by (dimension, value), keeping both the mgas array
    //    and the source test names so a 1-test bucket can deep-link to the
    //    test detail modal on click.
    type Bucket = { mgas: number[]; names: string[] }
    const byDim = new Map<string, Map<string, Bucket>>()
    const bump = (dim: string, value: string | undefined, name: string, mgas: number) => {
      if (!value) return
      let inner = byDim.get(dim)
      if (!inner) {
        inner = new Map()
        byDim.set(dim, inner)
      }
      let bucket = inner.get(value)
      if (!bucket) {
        bucket = { mgas: [], names: [] }
        inner.set(value, bucket)
      }
      bucket.mgas.push(mgas)
      bucket.names.push(name)
    }
    for (const s of samples) {
      const p = parseEESTName(s.name)
      if (!p.isEEST) continue
      bump('file', p.file, s.name, s.mgas)
      bump('fn', p.fn, s.name, s.mgas)
      bump('benchmark', p.benchmark, s.name, s.mgas)
      bump('opcode', p.opcode, s.name, s.mgas)
      bump('fork', p.fork, s.name, s.mgas)
      for (const { key, value } of p.params) bump(key, value, s.name, s.mgas)
      for (const label of p.labels) bump('label', label, s.name, s.mgas)
    }

    // 3. Order dimensions: canonical primary + discovered params (alpha) + trailing.
    const known = new Set([
      ...PRIMARY_DIMENSIONS.map((d) => d.key),
      ...TRAILING_DIMENSIONS.map((d) => d.key),
    ])
    const paramKeys = [...byDim.keys()].filter((k) => !known.has(k)).sort()
    const ordered: DimensionDef[] = [
      ...PRIMARY_DIMENSIONS,
      ...paramKeys.map((k) => ({ key: k, label: k, emitKey: k })),
      ...TRAILING_DIMENSIONS,
    ].filter((d) => byDim.has(d.key))

    // 4. Roll up per-value stats. Skip dimensions with only one value — no
    //    comparison to make.
    const result: DimensionAgg[] = []
    for (const def of ordered) {
      const inner = byDim.get(def.key)!
      if (inner.size < 2) continue

      const values: ValueAgg[] = []
      for (const [value, bucket] of inner) {
        const sorted = [...bucket.mgas].sort((a, b) => a - b)
        const mean = sorted.reduce((s, v) => s + v, 0) / sorted.length
        values.push({
          value,
          count: sorted.length,
          mean,
          p50: percentile(sorted, 50),
          p95: percentile(sorted, 95),
          p99: percentile(sorted, 99),
          singleTestName: bucket.names.length === 1 ? bucket.names[0] : undefined,
        })
      }
      result.push({ def, values })
    }

    return result
  }, [tests, stepFilter, searchQuery, statusFilter])

  // Pick a default dimension for the table view once we have data.
  const groupByDim = groupByKey ?? dimensions[0]?.def.key ?? null
  const tableDim = dimensions.find((d) => d.def.key === groupByDim) ?? dimensions[0]

  const sortedTableValues = useMemo(() => {
    if (!tableDim) return []
    const { col, dir } = tableSort
    const sign = dir === 'asc' ? 1 : -1
    return [...tableDim.values].sort((a, b) => {
      if (col === 'value') return sign * compareNumeric(a.value, b.value)
      return sign * (a[col] - b[col])
    })
  }, [tableDim, tableSort])

  const handleSort = (col: SortColumn) => {
    setTableSort((prev) => {
      if (prev.col === col) return { col, dir: prev.dir === 'asc' ? 'desc' : 'asc' }
      return { col, dir: col === 'value' ? 'asc' : 'desc' }
    })
  }

  // The page filter terms feeding this breakdown. Free-text terms (no `:`
  // or `=`) are kept too — they also affect what's counted.
  const activeTerms = splitQuery(query)

  return (
    <div className="overflow-hidden rounded-sm border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
      <div className="flex items-center gap-2 border-b border-gray-200 px-3 py-2 text-sm/6 font-medium text-gray-900 dark:border-gray-700 dark:text-gray-100">
        <BarChart3 className="size-4 text-gray-400 dark:text-gray-500" />
        Dimension breakdown
        <span className="text-xs/5 text-gray-500 dark:text-gray-400">
          ({dimensions.length} dimension{dimensions.length === 1 ? '' : 's'}
          {activeTerms.length > 0 && `, ${activeTerms.length} filter${activeTerms.length === 1 ? '' : 's'}`})
        </span>
      </div>
      <div className="flex flex-col gap-3 p-3">
          <div className="flex flex-wrap items-center gap-1.5 text-xs/5">
            <span className="text-[10px]/4 font-medium uppercase tracking-wide text-gray-400 dark:text-gray-500">
              Filters
            </span>
            {activeTerms.length === 0 ? (
              <span className="text-gray-500 dark:text-gray-400">none — showing all tests</span>
            ) : (
              activeTerms.map((term) => (
                <button
                  key={term}
                  type="button"
                  onClick={() => onToggle(term)}
                  title={`Click to remove ${term}`}
                  className="inline-flex cursor-pointer items-center gap-1 rounded-xs bg-blue-500 px-1.5 py-0 font-mono text-[11px]/5 text-white ring-1 ring-inset ring-blue-500 hover:bg-blue-600"
                >
                  <span>{term}</span>
                  <X className="size-3" />
                </button>
              ))
            )}
          </div>
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <div className="flex items-center gap-1 rounded-xs bg-gray-100 p-0.5 dark:bg-gray-700">
                {(['bars', 'table'] as const).map((v) => (
                  <button
                    key={v}
                    type="button"
                    onClick={() => setView(v)}
                    className={clsx(
                      'flex cursor-pointer items-center gap-1.5 rounded-xs px-2 py-1 text-xs/5 font-medium transition-colors',
                      view === v
                        ? 'bg-white text-gray-900 shadow-xs dark:bg-gray-600 dark:text-gray-100'
                        : 'text-gray-600 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100',
                    )}
                  >
                    {v === 'bars' ? <BarChart3 className="size-3.5" /> : <Table2 className="size-3.5" />}
                    {v === 'bars' ? 'Bars' : 'Table'}
                  </button>
                ))}
              </div>
              {view === 'bars' && (
                <button
                  type="button"
                  onClick={() => setBarsDir(barsDir === 'desc' ? 'asc' : 'desc')}
                  title={barsDir === 'desc' ? 'Click to sort slowest first' : 'Click to sort fastest first'}
                  className="flex cursor-pointer items-center gap-1 rounded-xs border border-gray-300 bg-white px-2 py-1 text-xs/5 font-medium text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600 dark:hover:text-gray-100"
                >
                  {barsDir === 'desc'
                    ? <><ChevronDown className="size-3.5" /> Fastest first</>
                    : <><ChevronUp className="size-3.5" /> Slowest first</>}
                </button>
              )}
            </div>
            {view === 'table' && tableDim && (
              <div className="flex items-center gap-2 text-xs/5 text-gray-500 dark:text-gray-400">
                <span>Group by:</span>
                <select
                  value={tableDim.def.key}
                  onChange={(e) => setGroupByKey(e.target.value)}
                  className="rounded-xs border border-gray-300 bg-white px-2 py-0.5 text-xs/5 text-gray-700 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-200"
                >
                  {dimensions.map((d) => (
                    <option key={d.def.key} value={d.def.key}>
                      {d.def.label} ({d.values.length})
                    </option>
                  ))}
                </select>
              </div>
            )}
          </div>

          {dimensions.length === 0 && (
            <div className="text-xs/5 text-gray-500 dark:text-gray-400">
              Not enough data — at least one dimension with two distinct values is required.
            </div>
          )}

          {view === 'bars' && (() => {
            // Force any dimension with an active filter into the preview so
            // users always see the dim they're filtering by.
            // Once a File filter is active, Test (fn) becomes the natural
            // next drill-down — surface it in the preview alongside File and
            // Gas without requiring "Show more dimensions".
            const hasFileFilter = activeTerms.some((t) => queryTermDimension(t) === 'file')
            const previewDims = dimensions.filter(({ def, values }) =>
              BARS_PREVIEW_KEYS.has(def.key) ||
              (hasFileFilter && def.key === 'fn') ||
              values.some((v) => searchQueryContains(query, `${def.emitKey}=${v.value}`)),
            )
            const hiddenDims = dimensions.filter((d) => !previewDims.includes(d))
            const visibleDims = showAllBars ? dimensions : previewDims

            const renderDim = ({ def, values }: DimensionAgg) => {
              const max = Math.max(...values.map((v) => v.mean))
              const sign = barsDir === 'desc' ? -1 : 1
              const sorted = [...values].sort((a, b) => sign * (a.mean - b.mean))
              return (
                <div key={def.key} className="flex flex-col gap-1">
                  <div className="text-[10px]/4 font-medium uppercase tracking-wide text-gray-400 dark:text-gray-500">
                    {def.label}
                    <span className="ml-1 lowercase text-gray-300 dark:text-gray-600">({sorted.length})</span>
                  </div>
                  <div className="flex flex-col gap-0.5">
                    {sorted.map((v) => {
                      const term = `${def.emitKey}=${v.value}`
                      const active = searchQueryContains(query, term)
                      const widthPct = max > 0 ? (v.mean / max) * 100 : 0
                      return (
                        <button
                          key={v.value}
                          type="button"
                          onClick={() => {
                            // If this value matches a single test (and the
                            // chip isn't already active), short-circuit and
                            // open that test's detail modal instead of
                            // adding a redundant filter that yields the
                            // same single test.
                            if (!active && v.singleTestName && onTestClick) {
                              onTestClick(v.singleTestName)
                            } else {
                              onToggle(term)
                            }
                          }}
                          title={
                            !active && v.singleTestName
                              ? `${def.label}: ${v.value} — open the only matching test (${v.mean.toFixed(1)} MGas/s)`
                              : `${def.label}: ${v.value} — mean ${v.mean.toFixed(1)} MGas/s, ${v.count} test${v.count === 1 ? '' : 's'}`
                          }
                          className={clsx(
                            'group grid cursor-pointer grid-cols-[minmax(7rem,12rem)_1fr_auto] items-center gap-2 rounded-xs px-1 py-0.5 text-left text-xs/5 transition-colors',
                            active ? 'bg-blue-50 text-blue-900 dark:bg-blue-950/40 dark:text-blue-200' : 'hover:bg-gray-50 dark:hover:bg-gray-700/50',
                          )}
                        >
                          <span className={clsx('truncate font-mono', active ? '' : 'text-gray-700 dark:text-gray-200')}>
                            {v.value}
                          </span>
                          <span className="relative h-3 w-full rounded-xs bg-gray-100 dark:bg-gray-700">
                            <span
                              className="absolute inset-y-0 left-0 rounded-xs"
                              style={{
                                width: `${widthPct}%`,
                                backgroundColor: getColorByThreshold(v.mean, threshold),
                                outline: active ? '1.5px solid #3b82f6' : 'none',
                                outlineOffset: '0px',
                              }}
                            />
                          </span>
                          <span className="flex shrink-0 items-baseline gap-1.5 font-mono tabular-nums text-gray-600 dark:text-gray-300">
                            <span>{v.mean.toFixed(1)}</span>
                            <span className="text-[10px]/4 text-gray-400 dark:text-gray-500">×{v.count}</span>
                          </span>
                        </button>
                      )
                    })}
                  </div>
                </div>
              )
            }

            return (
              <>
                {visibleDims.map(renderDim)}
                {hiddenDims.length > 0 && (
                  <button
                    type="button"
                    onClick={() => setShowAllBars(!showAllBars)}
                    className="flex w-fit cursor-pointer items-center gap-1 rounded-xs px-1.5 py-1 text-xs/5 font-medium text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-100"
                  >
                    {showAllBars
                      ? <><ChevronUp className="size-3.5" /> Show fewer dimensions</>
                      : <><ChevronDown className="size-3.5" /> Show more dimensions ({hiddenDims.length})</>}
                  </button>
                )}
              </>
            )
          })()}

          {view === 'table' && tableDim && (
            <div className="overflow-x-auto">
              <table className="w-full text-xs/5">
                <thead className="bg-gray-50 dark:bg-gray-900">
                  <tr>
                    <SortableTh label={tableDim.def.label} col="value" sort={tableSort} onSort={handleSort} align="left" />
                    <SortableTh label="Count" col="count" sort={tableSort} onSort={handleSort} align="right" />
                    <SortableTh label="Mean" col="mean" sort={tableSort} onSort={handleSort} align="right" />
                    <SortableTh label="P50" col="p50" sort={tableSort} onSort={handleSort} align="right" />
                    <SortableTh label="P95" col="p95" sort={tableSort} onSort={handleSort} align="right" />
                    <SortableTh label="P99" col="p99" sort={tableSort} onSort={handleSort} align="right" />
                  </tr>
                </thead>
                <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
                  {sortedTableValues.map((v) => {
                    const term = `${tableDim.def.emitKey}=${v.value}`
                    const active = searchQueryContains(query, term)
                    return (
                      <tr
                        key={v.value}
                        onClick={() => {
                          if (!active && v.singleTestName && onTestClick) {
                            onTestClick(v.singleTestName)
                          } else {
                            onToggle(term)
                          }
                        }}
                        className={clsx(
                          'cursor-pointer transition-colors',
                          active ? 'bg-blue-50 dark:bg-blue-950/40' : 'hover:bg-gray-50 dark:hover:bg-gray-700/50',
                        )}
                        title={
                          !active && v.singleTestName
                            ? `Open the only matching test`
                            : active ? `Click to remove ${term}` : `Click to filter by ${term}`
                        }
                      >
                        <td className="px-2 py-1 font-mono text-gray-700 dark:text-gray-200">{v.value}</td>
                        <td className="px-2 py-1 text-right font-mono tabular-nums text-gray-500 dark:text-gray-400">{v.count}</td>
                        <td className="px-2 py-1 text-right font-mono tabular-nums text-gray-700 dark:text-gray-200">{v.mean.toFixed(1)}</td>
                        <td className="px-2 py-1 text-right font-mono tabular-nums text-gray-500 dark:text-gray-400">{v.p50.toFixed(1)}</td>
                        <td className="px-2 py-1 text-right font-mono tabular-nums text-gray-500 dark:text-gray-400">{v.p95.toFixed(1)}</td>
                        <td className="px-2 py-1 text-right font-mono tabular-nums text-gray-500 dark:text-gray-400">{v.p99.toFixed(1)}</td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>
    </div>
  )
}

function SortableTh({
  label,
  col,
  sort,
  onSort,
  align,
}: {
  label: string
  col: SortColumn
  sort: { col: SortColumn; dir: 'asc' | 'desc' }
  onSort: (col: SortColumn) => void
  align: 'left' | 'right'
}) {
  const active = sort.col === col
  return (
    <th
      className={clsx(
        'cursor-pointer px-2 py-1.5 text-[10px]/4 font-medium uppercase tracking-wide text-gray-500 hover:text-gray-700 dark:text-gray-400 dark:hover:text-gray-200',
        align === 'left' ? 'text-left' : 'text-right',
      )}
      onClick={() => onSort(col)}
    >
      <span className="inline-flex items-center gap-0.5">
        {label}
        {active && (sort.dir === 'asc' ? <ChevronUp className="size-3" /> : <ChevronDown className="size-3" />)}
      </span>
    </th>
  )
}
