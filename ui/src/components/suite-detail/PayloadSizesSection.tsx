import { useMemo, useState } from 'react'
import clsx from 'clsx'
import { ChevronRight, BarChart3 } from 'lucide-react'
import type { SuiteTest } from '@/api/types'
import { compileQuery, TEST_FILTER_HINT } from '@/utils/eestNameFilter'
import { formatBytes } from '@/utils/format'

export type PayloadSortColumn = 'name' | 'uncompressed' | 'bal' | 'snappy' | 'pct_bal' | 'ratio'
export type PayloadSortDirection = 'asc' | 'desc'

interface PayloadSizesSectionProps {
  tests: SuiteTest[]
}

interface PayloadRow {
  name: string
  uncompressed: number
  bal: number
  snappy: number
  pctBal: number
  ratio: number
}

function toRow(t: SuiteTest): PayloadRow | null {
  const u = t.payload_size_bytes ?? 0
  const b = t.bal_size_bytes ?? 0
  const s = t.payload_size_bytes_snappy ?? 0
  if (u === 0 && b === 0 && s === 0) return null
  return {
    name: t.name,
    uncompressed: u,
    bal: b,
    snappy: s,
    pctBal: u > 0 ? (b / u) * 100 : 0,
    ratio: u > 0 ? s / u : 0,
  }
}

export function PayloadSizesSection({ tests }: PayloadSizesSectionProps) {
  const [expanded, setExpanded] = useState(false)
  const [query, setQuery] = useState('')
  const [sortBy, setSortBy] = useState<PayloadSortColumn>('uncompressed')
  const [sortDir, setSortDir] = useState<PayloadSortDirection>('desc')
  // setSortBy / setSortDir wired in Task 18
  void setSortBy
  void setSortDir

  const rows = useMemo(() => {
    const all: PayloadRow[] = []
    for (const t of tests) {
      const row = toRow(t)
      if (row) all.push(row)
    }
    return all
  }, [tests])

  const compiled = useMemo(() => compileQuery(query), [query])
  const filtered = useMemo(() => {
    if (!query.trim()) return rows
    return rows.filter((r) => compiled(r.name))
  }, [rows, compiled, query])

  const sorted = useMemo(() => {
    const sortFns: Record<PayloadSortColumn, (a: PayloadRow, b: PayloadRow) => number> = {
      name: (a, b) => a.name.localeCompare(b.name),
      uncompressed: (a, b) => a.uncompressed - b.uncompressed,
      bal: (a, b) => a.bal - b.bal,
      snappy: (a, b) => a.snappy - b.snappy,
      pct_bal: (a, b) => a.pctBal - b.pctBal,
      ratio: (a, b) => a.ratio - b.ratio,
    }
    const cmp = sortFns[sortBy]
    const out = [...filtered].sort(cmp)
    if (sortDir === 'desc') out.reverse()
    return out
  }, [filtered, sortBy, sortDir])

  if (rows.length === 0) return null

  const maxUncompressed = Math.max(...rows.map((r) => r.uncompressed))

  return (
    <>
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center gap-2 px-4 py-3 text-left text-sm/6 font-medium text-gray-900 hover:bg-gray-50 dark:text-gray-100 dark:hover:bg-gray-700/50"
      >
        <ChevronRight className={clsx('size-4 text-gray-500 transition-transform', expanded && 'rotate-90')} />
        <BarChart3 className="size-4 text-gray-400 dark:text-gray-500" />
        Payload Sizes
        <span className="text-xs/5 text-gray-500 dark:text-gray-400">
          · {rows.length} tests · max {formatBytes(maxUncompressed)}
        </span>
      </button>
      {expanded && (
        <div className="border-t border-gray-200 p-4 dark:border-gray-700">
          <div className="mb-3 flex items-center gap-2">
            <input
              type="text"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder={TEST_FILTER_HINT}
              className="flex-1 rounded-sm border border-gray-300 px-3 py-1.5 text-sm dark:border-gray-600 dark:bg-gray-800"
            />
            <span className="text-xs text-gray-500 dark:text-gray-400">
              {sorted.length} matching
            </span>
          </div>

          <div className="mb-4 h-64 rounded-sm border border-dashed border-gray-300 p-4 text-sm text-gray-500 dark:border-gray-700">
            Chart placeholder (Task 17 will fill this in)
          </div>

          <div className="rounded-sm border border-dashed border-gray-300 p-4 text-sm text-gray-500 dark:border-gray-700">
            Table placeholder (Task 18 will fill this in) — {sorted.length} rows
          </div>
        </div>
      )}
    </>
  )
}
