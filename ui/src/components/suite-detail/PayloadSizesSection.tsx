import { useMemo, useState } from 'react'
import clsx from 'clsx'
import { ChevronRight, BarChart3, ArrowDown, ArrowUp } from 'lucide-react'
import ReactECharts from 'echarts-for-react'
import type { SuiteTest } from '@/api/types'
import { compileQuery, TEST_FILTER_HINT } from '@/utils/eestNameFilter'
import { formatBytes } from '@/utils/format'
import { Pagination } from '@/components/shared/Pagination'

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
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)

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

  const pageStart = (page - 1) * pageSize
  const pageEnd = pageStart + pageSize
  const visible = sorted.slice(pageStart, pageEnd)
  const totalPages = Math.max(1, Math.ceil(sorted.length / pageSize))

  function header(label: string, col: PayloadSortColumn, rightAlign = false) {
    const active = sortBy === col
    const Icon = active ? (sortDir === 'desc' ? ArrowDown : ArrowUp) : null
    return (
      <button
        type="button"
        onClick={() => {
          if (sortBy === col) {
            setSortDir(sortDir === 'asc' ? 'desc' : 'asc')
          } else {
            setSortBy(col)
            setSortDir('desc')
          }
          setPage(1)
        }}
        className={clsx(
          'flex items-center gap-1 text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400',
          rightAlign && 'ml-auto',
        )}
      >
        {label}
        {Icon ? <Icon className="size-3" /> : null}
      </button>
    )
  }

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

          <div className="mb-4">
            <ReactECharts
              option={{
                grid: { left: 220, right: 24, top: 32, bottom: 60, containLabel: false },
                tooltip: {
                  trigger: 'axis',
                  axisPointer: { type: 'shadow' },
                  formatter: (params: Array<{ seriesName: string; data: number; name: string }>) => {
                    const name = params[0]?.name ?? ''
                    const lines = params.map((p) => `${p.seriesName}: ${formatBytes(p.data)}`)
                    return `<b>${name}</b><br/>${lines.join('<br/>')}`
                  },
                },
                legend: {
                  top: 0,
                  data: ['Non-BAL', 'BAL', 'Snappy'],
                },
                xAxis: {
                  type: 'value',
                  axisLabel: { formatter: (v: number) => formatBytes(v) },
                },
                yAxis: {
                  type: 'category',
                  data: sorted.map((r) => r.name),
                  inverse: true,
                  axisLabel: {
                    width: 200,
                    overflow: 'truncate',
                    fontSize: 11,
                  },
                },
                dataZoom: [
                  { type: 'slider', yAxisIndex: 0, start: 0, end: Math.min(100, (30 / Math.max(1, sorted.length)) * 100), width: 14, right: 4 },
                  { type: 'inside', yAxisIndex: 0, start: 0, end: Math.min(100, (30 / Math.max(1, sorted.length)) * 100) },
                ],
                series: [
                  {
                    name: 'Non-BAL',
                    type: 'bar',
                    stack: 'uncompressed',
                    itemStyle: { color: '#3b82f6' },
                    data: sorted.map((r) => Math.max(0, r.uncompressed - r.bal)),
                  },
                  {
                    name: 'BAL',
                    type: 'bar',
                    stack: 'uncompressed',
                    itemStyle: { color: '#f59e0b' },
                    data: sorted.map((r) => r.bal),
                  },
                  {
                    name: 'Snappy',
                    type: 'bar',
                    itemStyle: { color: '#10b981' },
                    data: sorted.map((r) => r.snappy),
                  },
                ],
              }}
              style={{ height: Math.max(280, Math.min(800, sorted.length * 22 + 80)) }}
              notMerge
            />
          </div>

          <div className="overflow-x-auto rounded-sm border border-gray-200 dark:border-gray-700">
            <table className="min-w-full">
              <thead className="bg-gray-50/50 dark:bg-gray-800/30">
                <tr>
                  <th className="px-3 py-2 text-left">{header('Test', 'name')}</th>
                  <th className="px-3 py-2 text-right">{header('Uncompressed', 'uncompressed', true)}</th>
                  <th className="px-3 py-2 text-right">{header('BAL', 'bal', true)}</th>
                  <th className="px-3 py-2 text-right">{header('Snappy', 'snappy', true)}</th>
                  <th className="px-3 py-2 text-right">{header('% BAL', 'pct_bal', true)}</th>
                  <th className="px-3 py-2 text-right">{header('Ratio', 'ratio', true)}</th>
                </tr>
              </thead>
              <tbody>
                {visible.map((r) => (
                  <tr key={r.name} className="border-t border-gray-200 dark:border-gray-700">
                    <td className="px-3 py-1.5 font-mono text-xs/5 text-gray-900 dark:text-gray-100">{r.name}</td>
                    <td className="px-3 py-1.5 text-right text-sm/6 text-gray-700 dark:text-gray-300">{formatBytes(r.uncompressed)}</td>
                    <td className="px-3 py-1.5 text-right text-sm/6 text-gray-700 dark:text-gray-300">{formatBytes(r.bal)}</td>
                    <td className="px-3 py-1.5 text-right text-sm/6 text-gray-700 dark:text-gray-300">{formatBytes(r.snappy)}</td>
                    <td className="px-3 py-1.5 text-right text-sm/6 text-gray-700 dark:text-gray-300">{r.pctBal.toFixed(1)}%</td>
                    <td className="px-3 py-1.5 text-right text-sm/6 text-gray-700 dark:text-gray-300">{(r.ratio * 100).toFixed(1)}%</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <div className="mt-3 flex items-center justify-end gap-3">
            <select
              value={pageSize}
              onChange={(e) => {
                setPageSize(parseInt(e.target.value, 10))
                setPage(1)
              }}
              className="rounded-sm border border-gray-300 px-2 py-1 text-xs dark:border-gray-600 dark:bg-gray-800"
            >
              {[20, 50, 100].map((s) => (
                <option key={s} value={s}>
                  {s} / page
                </option>
              ))}
            </select>
            <Pagination
              currentPage={page}
              totalPages={totalPages}
              onPageChange={setPage}
            />
          </div>
        </div>
      )}
    </>
  )
}
