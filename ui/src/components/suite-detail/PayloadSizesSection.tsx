import { useMemo, useState } from 'react'
import clsx from 'clsx'
import { ChevronRight, BarChart3 } from 'lucide-react'
import ReactECharts from 'echarts-for-react'
import type { SuiteTest } from '@/api/types'
import { compileQuery, TEST_FILTER_HINT } from '@/utils/eestNameFilter'
import { formatBytes } from '@/utils/format'

interface PayloadSizesSectionProps {
  tests: SuiteTest[]
}

interface PayloadRow {
  name: string
  uncompressed: number
  bal: number
  snappy: number
}

function sumArray(xs: number[] | undefined): number {
  if (!xs) return 0
  let total = 0
  for (const v of xs) total += v
  return total
}

// Page-level overview: rolls up the per-newPayload arrays from the
// `test` step into a single number per metric per test. Per-block and
// per-step breakdowns (plus the per-test totals as table columns) live
// on the test-details modal and the main tests table — this section is
// just the at-a-glance bar-chart visualization.
function toRow(t: SuiteTest): PayloadRow | null {
  const testStep = t.payload_sizes?.test
  if (!testStep) return null
  const u = sumArray(testStep.ssz_full)
  const b = sumArray(testStep.ssz_bal)
  const s = sumArray(testStep.ssz_full_snappy)
  if (u === 0 && b === 0 && s === 0) return null
  return { name: t.name, uncompressed: u, bal: b, snappy: s }
}

export function PayloadSizesSection({ tests }: PayloadSizesSectionProps) {
  const [expanded, setExpanded] = useState(false)
  const [query, setQuery] = useState('')

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

  // Stable sort for the chart so the visual order is meaningful — show
  // the largest payloads at the top.
  const sorted = useMemo(
    () => [...filtered].sort((a, b) => b.uncompressed - a.uncompressed),
    [filtered],
  )

  if (rows.length === 0) return null

  const maxUncompressed = Math.max(...rows.map((r) => r.uncompressed))

  return (
    <>
      <button
        type="button"
        onClick={() => setExpanded(!expanded)}
        aria-expanded={expanded}
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
      )}
    </>
  )
}
