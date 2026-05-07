import { useMemo, useState } from 'react'
import clsx from 'clsx'
import { ChevronDown, ChevronUp, Filter, X } from 'lucide-react'
import { parseEESTName } from '@/utils/eestName'
import {
  compileQuery,
  queryWithoutDimension,
  searchQueryContains,
  splitQuery,
} from '@/utils/eestNameFilter'

interface FacetPanelProps {
  /** Raw test names to derive facets from (typically Object.keys(result.tests)). */
  testNames: string[]
  /** Current search query string. */
  query: string
  /** Toggle a `key=value` term in the query. */
  onToggle: (term: string) => void
}

interface DimensionDef {
  /** Canonical key used in queries (`opcode`, `benchmark`, `mem_size`, ...). */
  key: string
  /** Display label shown in the section header. */
  label: string
  /** Term key actually emitted on click (e.g. `gas` aliases to `benchmark`). */
  emitKey: string
}

// Pull values out of a parsed test name keyed by canonical dimension.
function extractDimensionValues(name: string): Map<string, Set<string>> {
  const result = new Map<string, Set<string>>()
  const p = parseEESTName(name)
  if (!p.isEEST) return result

  const add = (dim: string, value: string | undefined) => {
    if (!value) return
    if (!result.has(dim)) result.set(dim, new Set())
    result.get(dim)!.add(value)
  }

  add('opcode', p.opcode)
  add('benchmark', p.benchmark)
  add('fork', p.fork)
  add('file', p.file)
  add('fn', p.fn)
  for (const { key, value } of p.params) add(key, value)
  for (const label of p.labels) add('label', label)

  return result
}

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

interface DimensionData {
  def: DimensionDef
  values: { value: string; count: number; active: boolean }[]
}

function compareValues(a: string, b: string): number {
  // Numeric (e.g. `120M`, `1024`, `0`): sort by leading number.
  const na = parseInt(a, 10)
  const nb = parseInt(b, 10)
  const aIsNum = !Number.isNaN(na) && /^\d/.test(a)
  const bIsNum = !Number.isNaN(nb) && /^\d/.test(b)
  if (aIsNum && bIsNum) return na - nb
  return a.localeCompare(b)
}

// Dimensions visible by default — the rest hide behind "Show more facets".
// Anything with an active filter is also forced into the preview so users
// can always see and clear what's pinned.
const PREVIEW_KEYS = new Set(['file', 'benchmark'])

export function FacetPanel({ testNames, query, onToggle }: FacetPanelProps) {
  const [showAll, setShowAll] = useState(false)

  const data = useMemo(() => {
    // Parse every test once.
    const parsed = testNames.map((n) => ({ name: n, dims: extractDimensionValues(n) }))

    // Discover all dimension keys actually present in this run, then merge
    // with the canonical orderings.
    const allKeys = new Set<string>()
    for (const p of parsed) for (const k of p.dims.keys()) allKeys.add(k)

    const knownKeys = new Set([
      ...PRIMARY_DIMENSIONS.map((d) => d.key),
      ...TRAILING_DIMENSIONS.map((d) => d.key),
    ])
    const paramKeys = [...allKeys].filter((k) => !knownKeys.has(k)).sort()

    const ordered: DimensionDef[] = [
      ...PRIMARY_DIMENSIONS,
      ...paramKeys.map((k) => ({ key: k, label: k, emitKey: k })),
      ...TRAILING_DIMENSIONS,
    ].filter((d) => allKeys.has(d.key))

    const dims: DimensionData[] = []
    for (const def of ordered) {
      // Contextual count: filter tests by the query EXCLUDING terms on this
      // dimension, so picking another opcode value doesn't show 0 counts
      // everywhere just because a sibling opcode is selected.
      const contextQuery = queryWithoutDimension(query, def.key)
      const matchesContext = contextQuery ? compileQuery(contextQuery) : null
      const counts = new Map<string, number>()

      for (const p of parsed) {
        if (matchesContext && !matchesContext(p.name)) continue
        const vs = p.dims.get(def.key)
        if (!vs) continue
        for (const v of vs) counts.set(v, (counts.get(v) ?? 0) + 1)
      }

      // Always include currently-active values even if their context count is
      // 0, so users can see and click to remove them.
      const activeValues = new Set<string>()
      for (const term of splitQuery(query)) {
        const sep = term.includes('=') ? '=' : term.includes(':') ? ':' : null
        if (!sep) continue
        const [k, v] = term.split(sep)
        const canonical = def.key === 'benchmark' && k.toLowerCase() === 'gas'
          ? 'benchmark'
          : k.toLowerCase()
        if (canonical === def.key) activeValues.add(v)
      }
      for (const av of activeValues) {
        if (!counts.has(av)) counts.set(av, 0)
      }

      const values = [...counts.entries()]
        .map(([value, count]) => ({
          value,
          count,
          active: searchQueryContains(query, `${def.emitKey}=${value}`),
        }))
        .sort((a, b) => {
          if (a.active !== b.active) return a.active ? -1 : 1
          return compareValues(a.value, b.value)
        })

      if (values.length > 0) dims.push({ def, values })
    }

    return dims
  }, [testNames, query])

  const totalActive = useMemo(
    () => splitQuery(query).filter((t) => /[:=]/.test(t)).length,
    [query],
  )

  // Force any dimension with an active filter into the preview so users
  // can always see and clear what's pinned, even if it lives in a
  // normally-hidden section.
  const previewDims = data.filter(({ def, values }) => PREVIEW_KEYS.has(def.key) || values.some((v) => v.active))
  const hiddenDims = data.filter((d) => !previewDims.includes(d))
  const visibleDims = showAll ? data : previewDims

  const renderDim = ({ def, values }: typeof data[number]) => (
    <div key={def.key} className="flex flex-col gap-1">
      <div className="text-[10px]/4 font-medium uppercase tracking-wide text-gray-400 dark:text-gray-500">
        {def.label}
        <span className="ml-1 lowercase text-gray-300 dark:text-gray-600">({values.length})</span>
      </div>
      <div className="flex flex-wrap gap-1">
        {values.map(({ value, count, active }) => {
          const term = `${def.emitKey}=${value}`
          const dimmed = !active && count === 0
          return (
            <button
              key={value}
              type="button"
              onClick={() => onToggle(term)}
              title={active ? `Click to remove ${term}` : `Click to filter by ${term}`}
              className={clsx(
                'inline-flex cursor-pointer items-center gap-1 rounded-xs px-1.5 py-0 font-mono text-[11px]/5 ring-1 ring-inset transition-colors',
                active
                  ? 'bg-blue-500 text-white ring-blue-500'
                  : dimmed
                    ? 'bg-gray-50 text-gray-400 ring-gray-200 dark:bg-gray-800 dark:text-gray-600 dark:ring-gray-700'
                    : 'bg-gray-100 text-gray-700 ring-gray-200 hover:bg-blue-100 hover:text-blue-700 hover:ring-blue-300 dark:bg-gray-700 dark:text-gray-200 dark:ring-gray-600 dark:hover:bg-blue-950/60 dark:hover:text-blue-300 dark:hover:ring-blue-700',
              )}
            >
              <span>{value}</span>
              <span className={clsx('font-mono text-[10px]/4', active ? 'text-blue-100' : 'text-gray-400 dark:text-gray-500')}>
                {count}
              </span>
              {active && <X className="size-3" />}
            </button>
          )
        })}
      </div>
    </div>
  )

  return (
    <div className="overflow-hidden rounded-sm border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
      <div className="flex items-center gap-2 border-b border-gray-200 px-3 py-2 text-sm/6 font-medium text-gray-900 dark:border-gray-700 dark:text-gray-100">
        <Filter className="size-4 text-gray-400 dark:text-gray-500" />
        Filter facets
        <span className="text-xs/5 text-gray-500 dark:text-gray-400">
          ({data.length} dimension{data.length === 1 ? '' : 's'}
          {totalActive > 0 && `, ${totalActive} active`})
        </span>
      </div>
      <div className="flex flex-col gap-3 p-3">
        {data.length === 0 && (
          <div className="text-xs/5 text-gray-500 dark:text-gray-400">
            No EEST-formatted test names in this run.
          </div>
        )}
        {visibleDims.map(renderDim)}
        {hiddenDims.length > 0 && (
          <button
            type="button"
            onClick={() => setShowAll(!showAll)}
            className="flex w-fit cursor-pointer items-center gap-1 rounded-xs px-1.5 py-1 text-xs/5 font-medium text-gray-600 hover:bg-gray-100 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-700 dark:hover:text-gray-100"
          >
            {showAll
              ? <><ChevronUp className="size-3.5" /> Show fewer facets</>
              : <><ChevronDown className="size-3.5" /> Show more facets ({hiddenDims.length})</>}
          </button>
        )}
      </div>
    </div>
  )
}
