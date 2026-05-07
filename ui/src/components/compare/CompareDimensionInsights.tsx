import { useMemo, useState } from 'react'
import clsx from 'clsx'
import { BarChart3, ChevronDown, ChevronUp, Table2, X } from 'lucide-react'
import { parseEESTName } from '@/utils/eestName'
import { queryTermDimension, searchQueryContains, splitQuery } from '@/utils/eestNameFilter'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { type CompareRun, type LabelMode, RUN_SLOTS, formatRunLabel } from './constants'

interface CompareDimensionInsightsProps {
  runs: CompareRun[]
  stepFilter: StepTypeOption[]
  baselineIdx: number
  labelMode: LabelMode
  testNameFilter?: (name: string) => boolean
  /** Current search query, used to highlight bars/rows whose value is pinned. */
  query: string
  /** Toggle a `key=value` term in the page-level search. */
  onToggle: (term: string) => void
  /** Open the test detail modal for a specific test. */
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

const BARS_PREVIEW_KEYS = new Set(['file', 'benchmark'])

/** Per-(dim, value) per-run aggregate. */
interface RunAgg {
  /** Mean MGas/s of all tests with this value, in this run. */
  mean: number | undefined
  /** How many tests in this bucket from this run. */
  count: number
  /** Single test's name when count === 1, for "click → open modal". */
  singleTestName?: string
}

interface ValueAgg {
  value: string
  perRun: RunAgg[]
  /** Smallest count across runs (for the chip count display). */
  minCount: number
  /** Largest count across runs. */
  maxCount: number
  /** Largest baseline-relative absolute delta % across non-baseline runs. */
  maxAbsDeltaPct: number
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

/**
 * Map a baseline-relative delta % to a colour.
 * Negative (slower than baseline) → red shades.
 * Positive (faster than baseline) → green shades.
 * Zero → grey.
 */
function deltaColor(pct: number | undefined): string {
  if (pct === undefined || !isFinite(pct)) return '#9ca3af'
  if (Math.abs(pct) < 0.5) return '#9ca3af'
  if (pct >= 25) return '#16a34a'  // green-600
  if (pct >= 10) return '#22c55e'  // green-500
  if (pct >= 0) return '#86efac'   // green-300
  if (pct >= -10) return '#fca5a5' // red-300
  if (pct >= -25) return '#ef4444' // red-500
  return '#dc2626'                 // red-600
}

function formatDelta(pct: number | undefined): string {
  if (pct === undefined || !isFinite(pct)) return '—'
  if (Math.abs(pct) < 0.05) return '0%'
  const sign = pct >= 0 ? '+' : ''
  return `${sign}${pct.toFixed(1)}%`
}

type SortMode = { col: 'value' | 'count' | `run_${number}` | `delta_${number}`; dir: 'asc' | 'desc' }

export function CompareDimensionInsights({
  runs,
  stepFilter,
  baselineIdx,
  labelMode,
  testNameFilter,
  query,
  onToggle,
  onTestClick,
}: CompareDimensionInsightsProps) {
  const [view, setView] = useState<'bars' | 'table'>('bars')
  const [barsDir, setBarsDir] = useState<'desc' | 'asc'>('desc')
  const [showAllBars, setShowAllBars] = useState(false)
  const [groupByKey, setGroupByKey] = useState<string | null>(null)
  const [tableSort, setTableSort] = useState<SortMode>({ col: `delta_${runs.findIndex((_, i) => i !== baselineIdx) || 0}`, dir: 'asc' })

  const dimensions = useMemo<DimensionAgg[]>(() => {
    // 1. Per-run, build per-test mgas samples.
    const perRunSamples = runs.map((r) => {
      if (!r.result) return [] as { name: string; mgas: number }[]
      const out: { name: string; mgas: number }[] = []
      for (const [name, entry] of Object.entries(r.result.tests)) {
        if (testNameFilter && !testNameFilter(name)) continue
        const stats = getAggregatedStats(entry, stepFilter)
        if (!stats) continue
        const mgas = calculateMGasPerSec(stats.gas_used_total, stats.gas_used_time_total)
        if (mgas === undefined) continue
        out.push({ name, mgas })
      }
      return out
    })

    // 2. Bucket by (dim, value, run) — keep sums + counts + names for single-test deep-link.
    type Bucket = { sum: number; count: number; names: string[] }
    const byDim = new Map<string, Map<string, Bucket[]>>()
    const ensureBuckets = (dim: string, value: string) => {
      let inner = byDim.get(dim)
      if (!inner) {
        inner = new Map()
        byDim.set(dim, inner)
      }
      let arr = inner.get(value)
      if (!arr) {
        arr = Array.from({ length: runs.length }, () => ({ sum: 0, count: 0, names: [] }))
        inner.set(value, arr)
      }
      return arr
    }
    const bump = (runIdx: number, dim: string, value: string | undefined, name: string, mgas: number) => {
      if (!value) return
      const buckets = ensureBuckets(dim, value)
      buckets[runIdx].sum += mgas
      buckets[runIdx].count++
      buckets[runIdx].names.push(name)
    }
    for (let runIdx = 0; runIdx < runs.length; runIdx++) {
      for (const s of perRunSamples[runIdx]) {
        const p = parseEESTName(s.name)
        if (!p.isEEST) continue
        bump(runIdx, 'file', p.file, s.name, s.mgas)
        bump(runIdx, 'fn', p.fn, s.name, s.mgas)
        bump(runIdx, 'benchmark', p.benchmark, s.name, s.mgas)
        bump(runIdx, 'opcode', p.opcode, s.name, s.mgas)
        bump(runIdx, 'fork', p.fork, s.name, s.mgas)
        for (const { key, value } of p.params) bump(runIdx, key, value, s.name, s.mgas)
        for (const label of p.labels) bump(runIdx, 'label', label, s.name, s.mgas)
      }
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

    // 4. Roll up per-value stats across runs. Skip dims with only one value.
    const result: DimensionAgg[] = []
    for (const def of ordered) {
      const inner = byDim.get(def.key)!
      if (inner.size < 2) continue

      const values: ValueAgg[] = []
      for (const [value, buckets] of inner) {
        const perRun: RunAgg[] = buckets.map((b) => ({
          mean: b.count > 0 ? b.sum / b.count : undefined,
          count: b.count,
          singleTestName: b.count === 1 ? b.names[0] : undefined,
        }))
        const counts = perRun.map((r) => r.count)
        const minCount = Math.min(...counts)
        const maxCount = Math.max(...counts)
        const baseline = perRun[baselineIdx]?.mean
        let maxAbs = 0
        if (baseline !== undefined && baseline > 0) {
          for (let i = 0; i < perRun.length; i++) {
            if (i === baselineIdx) continue
            const m = perRun[i].mean
            if (m === undefined) continue
            const pct = ((m - baseline) / baseline) * 100
            if (Math.abs(pct) > maxAbs) maxAbs = Math.abs(pct)
          }
        }
        values.push({ value, perRun, minCount, maxCount, maxAbsDeltaPct: maxAbs })
      }
      result.push({ def, values })
    }

    return result
  }, [runs, stepFilter, testNameFilter, baselineIdx])

  // When the filter narrows down to a single unique test across all runs
  // there's nothing to break down — surface a deep-link to the test detail
  // modal instead of the otherwise-useless empty state.
  const lonelyTestName = useMemo<string | null>(() => {
    const names = new Set<string>()
    for (const r of runs) {
      if (!r.result) continue
      for (const name of Object.keys(r.result.tests)) {
        if (testNameFilter && !testNameFilter(name)) continue
        names.add(name)
        if (names.size > 1) return null
      }
    }
    return names.size === 1 ? [...names][0] : null
  }, [runs, testNameFilter])

  // Picker default: first dim that exists.
  const groupByDim = groupByKey ?? dimensions[0]?.def.key ?? null
  const tableDim = dimensions.find((d) => d.def.key === groupByDim) ?? dimensions[0]

  const sortedTableValues = useMemo(() => {
    if (!tableDim) return []
    const { col, dir } = tableSort
    const sign = dir === 'asc' ? 1 : -1
    return [...tableDim.values].sort((a, b) => {
      if (col === 'value') return sign * compareNumeric(a.value, b.value)
      if (col === 'count') return sign * (a.maxCount - b.maxCount)
      if (col.startsWith('run_')) {
        const idx = parseInt(col.slice(4), 10)
        const am = a.perRun[idx]?.mean ?? -Infinity
        const bm = b.perRun[idx]?.mean ?? -Infinity
        return sign * (am - bm)
      }
      // delta_<i>: sort by signed delta vs baseline (most negative first when desc=asc means most negative first → flip dir interpretation).
      const idx = parseInt(col.slice(6), 10)
      const baseA = a.perRun[baselineIdx]?.mean
      const baseB = b.perRun[baselineIdx]?.mean
      const ma = a.perRun[idx]?.mean
      const mb = b.perRun[idx]?.mean
      const da = baseA && baseA > 0 && ma !== undefined ? ((ma - baseA) / baseA) * 100 : 0
      const db = baseB && baseB > 0 && mb !== undefined ? ((mb - baseB) / baseB) * 100 : 0
      return sign * (da - db)
    })
  }, [tableDim, tableSort, baselineIdx])

  const handleSort = (col: SortMode['col']) => {
    setTableSort((prev) => {
      if (prev.col === col) return { col, dir: prev.dir === 'asc' ? 'desc' : 'asc' }
      // Default direction depends on column type — ascending for value/delta (most regressed first),
      // descending for count/per-run mean (largest first).
      return { col, dir: col === 'value' || col.startsWith('delta_') ? 'asc' : 'desc' }
    })
  }

  const activeTerms = splitQuery(query)
  const hasFileFilter = activeTerms.some((t) => queryTermDimension(t) === 'file')
  const previewDims = dimensions.filter(({ def, values }) =>
    BARS_PREVIEW_KEYS.has(def.key) ||
    (hasFileFilter && def.key === 'fn') ||
    values.some((v) => searchQueryContains(query, `${def.emitKey}=${v.value}`)),
  )
  const hiddenDims = dimensions.filter((d) => !previewDims.includes(d))
  const visibleDims = showAllBars ? dimensions : previewDims

  const renderBarsForValue = (def: DimensionDef, v: ValueAgg) => {
    const term = `${def.emitKey}=${v.value}`
    const active = searchQueryContains(query, term)
    const baseline = v.perRun[baselineIdx]?.mean
    const allMeans = v.perRun.map((r) => r.mean ?? 0)
    const maxMean = Math.max(...allMeans, 1)

    const onClick = () => {
      // If the entire row collapses to a single test (every run has 0 or 1
      // test, with at most one test name across runs), open it.
      const uniqueNames = new Set<string>()
      for (const r of v.perRun) for (const n of r.singleTestName ? [r.singleTestName] : []) uniqueNames.add(n)
      if (!active && onTestClick && uniqueNames.size === 1 && v.maxCount === 1) {
        onTestClick([...uniqueNames][0])
      } else {
        onToggle(term)
      }
    }

    return (
      <button
        key={v.value}
        type="button"
        onClick={onClick}
        title={active ? `Click to remove ${term}` : `Click to filter by ${term}`}
        className={clsx(
          'group flex w-full cursor-pointer flex-col gap-0.5 rounded-xs px-1 py-0.5 text-left text-xs/5 transition-colors',
          active ? 'bg-blue-50 dark:bg-blue-950/40' : 'hover:bg-gray-50 dark:hover:bg-gray-700/50',
        )}
      >
        <div className="flex items-baseline gap-2">
          <span className={clsx('truncate font-mono', active ? 'text-blue-900 dark:text-blue-200' : 'text-gray-700 dark:text-gray-200')}>
            {v.value}
          </span>
          <span className="text-[10px]/4 text-gray-400 dark:text-gray-500">
            ×{v.minCount === v.maxCount ? v.maxCount : `${v.minCount}-${v.maxCount}`}
          </span>
        </div>
        <div className="flex flex-col gap-0.5">
          {v.perRun.map((r, i) => {
            const slot = RUN_SLOTS[i] ?? RUN_SLOTS[0]
            const widthPct = r.mean !== undefined ? (r.mean / maxMean) * 100 : 0
            const pct = i !== baselineIdx && baseline !== undefined && baseline > 0 && r.mean !== undefined
              ? ((r.mean - baseline) / baseline) * 100
              : undefined
            const barColor = i === baselineIdx ? slot.color : deltaColor(pct)
            return (
              <div key={i} className="grid grid-cols-[8rem_1fr_auto_3rem] items-center gap-2">
                <span className="flex min-w-0 items-center gap-1 text-[10px]/4 text-gray-500 dark:text-gray-400" title={formatRunLabel(slot, runs[i], labelMode)}>
                  <img
                    src={`/img/clients/${runs[i].config.instance.client}.jpg`}
                    alt={runs[i].config.instance.client}
                    className="size-3.5 shrink-0 rounded-full object-cover"
                  />
                  <span className="truncate">{formatRunLabel(slot, runs[i], labelMode)}</span>
                  {i === baselineIdx && <span className="shrink-0 text-amber-500" title="Baseline">★</span>}
                </span>
                <span className="relative h-2.5 rounded-xs bg-gray-100 dark:bg-gray-700">
                  <span
                    className="absolute inset-y-0 left-0 rounded-xs"
                    style={{ width: `${widthPct}%`, backgroundColor: barColor }}
                  />
                </span>
                <span className="font-mono tabular-nums text-gray-600 dark:text-gray-300">
                  {r.mean !== undefined ? r.mean.toFixed(1) : '—'}
                </span>
                <span className="font-mono tabular-nums text-[10px]/4" style={{ color: i === baselineIdx ? '#9ca3af' : deltaColor(pct) }}>
                  {i === baselineIdx ? '' : formatDelta(pct)}
                </span>
              </div>
            )
          })}
        </div>
      </button>
    )
  }

  const renderDim = ({ def, values }: DimensionAgg) => {
    const sign = barsDir === 'desc' ? -1 : 1
    const sorted = [...values].sort((a, b) => sign * (a.maxAbsDeltaPct - b.maxAbsDeltaPct))
    return (
      <div key={def.key} className="flex flex-col gap-1">
        <div className="text-[10px]/4 font-medium uppercase tracking-wide text-gray-400 dark:text-gray-500">
          {def.label}
          <span className="ml-1 lowercase text-gray-300 dark:text-gray-600">({sorted.length})</span>
        </div>
        <div className="flex flex-col gap-1">
          {sorted.map((v) => renderBarsForValue(def, v))}
        </div>
      </div>
    )
  }

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
          <span className="text-[10px]/4 font-medium uppercase tracking-wide text-gray-400 dark:text-gray-500">Filters</span>
          {activeTerms.length === 0 ? (
            <span className="text-gray-500 dark:text-gray-400">none — comparing all tests</span>
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
                title={barsDir === 'desc' ? 'Click to sort smallest delta first' : 'Click to sort largest delta first'}
                className="flex cursor-pointer items-center gap-1 rounded-xs border border-gray-300 bg-white px-2 py-1 text-xs/5 font-medium text-gray-600 hover:bg-gray-50 hover:text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600 dark:hover:text-gray-100"
              >
                {barsDir === 'desc'
                  ? <><ChevronDown className="size-3.5" /> Largest delta first</>
                  : <><ChevronUp className="size-3.5" /> Smallest delta first</>}
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
          lonelyTestName && onTestClick ? (
            <button
              type="button"
              onClick={() => onTestClick(lonelyTestName)}
              className="flex w-fit cursor-pointer items-center gap-1 rounded-xs px-1.5 py-1 text-xs/5 font-medium text-blue-600 hover:bg-blue-50 hover:text-blue-800 dark:text-blue-400 dark:hover:bg-blue-950/40 dark:hover:text-blue-300"
            >
              Only one test matches — click to open its detail
            </button>
          ) : (
            <div className="text-xs/5 text-gray-500 dark:text-gray-400">
              Not enough data — at least one dimension with two distinct values is required.
            </div>
          )
        )}

        {view === 'bars' && visibleDims.map(renderDim)}
        {view === 'bars' && hiddenDims.length > 0 && (
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

        {view === 'table' && tableDim && (
          <div className="overflow-x-auto">
            <table className="w-full text-xs/5">
              <thead className="bg-gray-50 dark:bg-gray-900">
                <tr>
                  <SortableTh label={tableDim.def.label} col="value" sort={tableSort} onSort={handleSort} align="left" />
                  <SortableTh label="Count" col="count" sort={tableSort} onSort={handleSort} align="right" />
                  {runs.map((run, i) => {
                    const slot = RUN_SLOTS[i] ?? RUN_SLOTS[0]
                    const isBaseline = i === baselineIdx
                    return (
                      <SortableTh
                        key={`run-${i}`}
                        col={`run_${i}`}
                        sort={tableSort}
                        onSort={handleSort}
                        align="right"
                      >
                        <span className="inline-flex items-center gap-1">
                          <img
                            src={`/img/clients/${run.config.instance.client}.jpg`}
                            alt={run.config.instance.client}
                            className="size-3.5 rounded-full object-cover"
                          />
                          <span>{formatRunLabel(slot, run, labelMode)}</span>
                          {isBaseline && <span className="text-amber-500" title="Baseline">★</span>}
                        </span>
                      </SortableTh>
                    )
                  })}
                  {runs.map((run, i) => {
                    if (i === baselineIdx) return null
                    const slot = RUN_SLOTS[i] ?? RUN_SLOTS[0]
                    return (
                      <SortableTh
                        key={`delta-${i}`}
                        col={`delta_${i}`}
                        sort={tableSort}
                        onSort={handleSort}
                        align="right"
                      >
                        <span className="inline-flex items-center gap-1">
                          <span>Δ</span>
                          <img
                            src={`/img/clients/${run.config.instance.client}.jpg`}
                            alt={run.config.instance.client}
                            className="size-3.5 rounded-full object-cover"
                          />
                          <span>{formatRunLabel(slot, run, labelMode)}</span>
                        </span>
                      </SortableTh>
                    )
                  })}
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-200 dark:divide-gray-700">
                {sortedTableValues.map((v) => {
                  const term = `${tableDim.def.emitKey}=${v.value}`
                  const active = searchQueryContains(query, term)
                  const baseline = v.perRun[baselineIdx]?.mean
                  const onClick = () => {
                    const uniqueNames = new Set<string>()
                    for (const r of v.perRun) for (const n of r.singleTestName ? [r.singleTestName] : []) uniqueNames.add(n)
                    if (!active && onTestClick && uniqueNames.size === 1 && v.maxCount === 1) {
                      onTestClick([...uniqueNames][0])
                    } else {
                      onToggle(term)
                    }
                  }
                  return (
                    <tr
                      key={v.value}
                      onClick={onClick}
                      className={clsx(
                        'cursor-pointer transition-colors',
                        active ? 'bg-blue-50 dark:bg-blue-950/40' : 'hover:bg-gray-50 dark:hover:bg-gray-700/50',
                      )}
                      title={active ? `Click to remove ${term}` : `Click to filter by ${term}`}
                    >
                      <td className="px-2 py-1 font-mono text-gray-700 dark:text-gray-200">{v.value}</td>
                      <td className="px-2 py-1 text-right font-mono tabular-nums text-gray-500 dark:text-gray-400">
                        {v.minCount === v.maxCount ? v.maxCount : `${v.minCount}-${v.maxCount}`}
                      </td>
                      {v.perRun.map((r, i) => (
                        <td key={`run-${i}`} className="px-2 py-1 text-right font-mono tabular-nums text-gray-700 dark:text-gray-200">
                          {r.mean !== undefined ? r.mean.toFixed(1) : '—'}
                        </td>
                      ))}
                      {v.perRun.map((r, i) => {
                        if (i === baselineIdx) return null
                        const pct = baseline !== undefined && baseline > 0 && r.mean !== undefined
                          ? ((r.mean - baseline) / baseline) * 100
                          : undefined
                        return (
                          <td
                            key={`delta-${i}`}
                            className="px-2 py-1 text-right font-mono tabular-nums"
                            style={{ color: deltaColor(pct) }}
                          >
                            {formatDelta(pct)}
                          </td>
                        )
                      })}
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
  children,
  col,
  sort,
  onSort,
  align,
}: {
  label?: string
  children?: React.ReactNode
  col: SortMode['col']
  sort: SortMode
  onSort: (col: SortMode['col']) => void
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
        {children ?? label}
        {active && (sort.dir === 'asc' ? <ChevronUp className="size-3" /> : <ChevronDown className="size-3" />)}
      </span>
    </th>
  )
}
