import { useEffect, useMemo, useRef, useState } from 'react'
import clsx from 'clsx'
import { ListOrdered } from 'lucide-react'
import ReactECharts from 'echarts-for-react'
import type { SuiteTest } from '@/api/types'
import { compileQuery } from '@/utils/eestNameFilter'
import { formatTestNameLong } from '@/utils/eestName'
import { useNameDisplayMode } from '@/hooks/useNameDisplayMode'

// Reactive dark-mode detector — mirrors PayloadSizesSection so the
// chart's tooltip theming flips with the rest of the page.
function useDarkMode() {
  const [isDark, setIsDark] = useState(() =>
    typeof document !== 'undefined' && document.documentElement.classList.contains('dark'),
  )
  useEffect(() => {
    const observer = new MutationObserver(() => {
      setIsDark(document.documentElement.classList.contains('dark'))
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [])
  return isDark
}

function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => {
    switch (c) {
      case '&': return '&amp;'
      case '<': return '&lt;'
      case '>': return '&gt;'
      case '"': return '&quot;'
      default: return '&#39;'
    }
  })
}

type ChartOrder = 'index' | 'count'

interface TxCountsSectionProps {
  tests: SuiteTest[]
  /** Called when a bar is clicked, with the 1-based test index. */
  onTestClick?: (index: number) => void
  /**
   * External search query that filters which tests appear in the chart.
   * Controlled by the parent so the same query also drives the tests
   * table and other sections on the suite-details page.
   */
  searchQuery?: string
  order?: ChartOrder
  onOrderChange?: (order: ChartOrder) => void
}

interface TxRow {
  index: number // 1-based position in the suite, matches the # column in the tests table.
  name: string
  total: number // sum of tx counts across all newPayloads in the test step.
  blocks: number // number of newPayloads in the test step.
}

function sumArray(xs: number[] | undefined): number {
  if (!xs) return 0
  let total = 0
  for (const v of xs) total += v
  return total
}

// Rolls up the per-newPayload tx counts from the `test` step into a
// single number per test. The full per-block / per-step breakdown lives
// on the test-details modal; this section is the at-a-glance bar chart.
function toRow(t: SuiteTest, index: number): TxRow | null {
  const testStep = t.tx_counts?.test
  if (!testStep || testStep.length === 0) return null
  const total = sumArray(testStep)
  return { index, name: t.name, total, blocks: testStep.length }
}

export function TxCountsSection({
  tests,
  onTestClick,
  searchQuery = '',
  order: orderProp,
  onOrderChange,
}: TxCountsSectionProps) {
  const [localOrder, setLocalOrder] = useState<ChartOrder>('index')
  const order = orderProp ?? localOrder
  const setOrder = (next: ChartOrder) => {
    if (onOrderChange) onOrderChange(next)
    else setLocalOrder(next)
  }
  const isDark = useDarkMode()
  const { mode: nameMode } = useNameDisplayMode()
  const chartRef = useRef<ReactECharts | null>(null)

  const rows = useMemo(() => {
    const all: TxRow[] = []
    for (let i = 0; i < tests.length; i++) {
      const row = toRow(tests[i], i + 1)
      if (row) all.push(row)
    }
    return all
  }, [tests])

  const compiled = useMemo(() => compileQuery(searchQuery), [searchQuery])
  const filtered = useMemo(() => {
    if (!searchQuery.trim()) return rows
    return rows.filter((r) => compiled(r.name))
  }, [rows, compiled, searchQuery])

  const ordered = useMemo(
    () => order === 'count' ? [...filtered].sort((a, b) => b.total - a.total) : filtered,
    [filtered, order],
  )

  const categories = useMemo(() => ordered.map((r) => String(r.index)), [ordered])
  const indexToRow = useMemo(
    () => new Map(ordered.map((r) => [String(r.index), r])),
    [ordered],
  )

  // Click-anywhere-in-a-column → open the test modal (same pattern as
  // PayloadSizesSection, since ECharts' built-in click misses gaps).
  useEffect(() => {
    if (!onTestClick) return
    const inst = chartRef.current?.getEchartsInstance()
    if (!inst) return
    const zr = inst.getZr()
    const handler = (event: { offsetX: number; offsetY: number }) => {
      const pixel: [number, number] = [event.offsetX, event.offsetY]
      if (!inst.containPixel('grid', pixel)) return
      const raw = inst.convertFromPixel({ xAxisIndex: 0 }, event.offsetX) as number
      if (typeof raw !== 'number' || !Number.isFinite(raw)) return
      const i = Math.round(raw)
      if (i < 0 || i >= categories.length) return
      const idx = parseInt(categories[i], 10)
      if (Number.isFinite(idx) && idx > 0) onTestClick(idx)
    }
    zr.on('click', handler)
    return () => zr.off('click', handler)
  }, [onTestClick, categories])

  if (rows.length === 0) return null

  const maxTotal = Math.max(...rows.map((r) => r.total))

  return (
    <>
      <div className="flex w-full items-center gap-2 border-b border-gray-200 px-4 py-3 text-sm/6 font-medium text-gray-900 dark:border-gray-700 dark:text-gray-100">
        <ListOrdered className="size-4 text-gray-400 dark:text-gray-500" />
        Transaction Counts
        <span className="text-xs/5 text-gray-500 dark:text-gray-400">
          · {rows.length} tests · max {maxTotal.toLocaleString()} txs
        </span>
      </div>
      <div className="p-4">
        <div className="mb-3 flex items-center justify-end gap-2">
          {searchQuery.trim() && (
            <span className="text-xs text-gray-500 dark:text-gray-400">
              {filtered.length} of {rows.length} matching
            </span>
          )}
          <div className="flex shrink-0 items-center gap-1 rounded-sm border border-gray-300 bg-white p-0.5 text-xs/5 dark:border-gray-600 dark:bg-gray-800">
            <span className="px-1 text-gray-500 dark:text-gray-400">Order:</span>
            {([
              { value: 'index', label: 'Test #' },
              { value: 'count', label: 'Tx count' },
            ] as const).map((opt) => (
              <button
                key={opt.value}
                type="button"
                onClick={() => setOrder(opt.value)}
                className={clsx(
                  'cursor-pointer rounded-xs px-2 py-1 font-medium transition-colors',
                  order === opt.value
                    ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                    : 'text-gray-500 hover:bg-gray-100 dark:text-gray-400 dark:hover:bg-gray-700',
                )}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>

        <ReactECharts
          ref={chartRef}
          option={(() => {
            const textColor = isDark ? '#ffffff' : '#374151'
            const mutedTextColor = isDark ? '#9ca3af' : '#6b7280'
            const axisLineColor = isDark ? '#4b5563' : '#d1d5db'
            const splitLineColor = isDark ? '#374151' : '#e5e7eb'
            return {
              backgroundColor: 'transparent',
              textStyle: { color: textColor },
              grid: { left: 70, right: 24, top: 16, bottom: 55, containLabel: false },
              tooltip: {
                trigger: 'axis',
                axisPointer: { type: 'shadow' },
                backgroundColor: isDark ? '#1f2937' : '#ffffff',
                borderColor: isDark ? '#374151' : '#e5e7eb',
                textStyle: { color: textColor },
                confine: true,
                extraCssText: 'max-width: 300px; white-space: normal;',
                formatter: (params: Array<{ color: string; data: number; name: string }>) => {
                  if (!params.length) return ''
                  const idx = params[0].name ?? ''
                  const row = indexToRow.get(idx)
                  const rawName = row?.name ?? ''
                  const decomposedSafe = escapeHtml(formatTestNameLong(rawName, nameMode))
                  let content = `<strong>Test #${escapeHtml(idx)}</strong>`
                  content += `<br/><span style="font-size:11px;color:${mutedTextColor};word-break:break-all;display:block;">${decomposedSafe}</span><br/>`
                  content += `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${params[0].color};margin-right:6px;"></span>`
                  content += `Transactions: ${params[0].data.toLocaleString()}`
                  if (row) {
                    content += `<br/><span style="font-size:11px;color:${mutedTextColor};">${row.blocks} block${row.blocks === 1 ? '' : 's'}</span>`
                  }
                  return content
                },
              },
              xAxis: {
                type: 'category',
                data: categories,
                axisLabel: {
                  color: textColor,
                  fontSize: 10,
                  formatter: (v: string) => `#${v}`,
                },
                axisLine: { lineStyle: { color: axisLineColor } },
                axisTick: { lineStyle: { color: axisLineColor } },
              },
              yAxis: {
                type: 'value',
                axisLabel: { color: textColor, formatter: (v: number) => v.toLocaleString() },
                axisLine: { show: true, lineStyle: { color: axisLineColor } },
                splitLine: { lineStyle: { color: splitLineColor } },
              },
              dataZoom: [
                {
                  type: 'slider',
                  xAxisIndex: 0,
                  start: 0,
                  end: 100,
                  height: 14,
                  bottom: 30,
                  borderColor: axisLineColor,
                  fillerColor: isDark ? 'rgba(139, 92, 246, 0.3)' : 'rgba(139, 92, 246, 0.1)',
                  backgroundColor: isDark ? '#374151' : '#f3f4f6',
                  textStyle: { color: textColor },
                },
                { type: 'inside', xAxisIndex: 0, start: 0, end: 100 },
              ],
              series: [
                {
                  name: 'Transactions',
                  type: 'bar',
                  itemStyle: { color: '#8b5cf6' },
                  data: ordered.map((r) => r.total),
                },
              ],
            }
          })()}
          style={{ height: 320 }}
          notMerge
        />
      </div>
    </>
  )
}
