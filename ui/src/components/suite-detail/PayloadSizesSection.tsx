import { useEffect, useMemo, useRef, useState } from 'react'
import clsx from 'clsx'
import { BarChart3 } from 'lucide-react'
import ReactECharts from 'echarts-for-react'
import type { SuiteTest } from '@/api/types'
import { compileQuery } from '@/utils/eestNameFilter'
import { formatTestNameLong } from '@/utils/eestName'
import { useNameDisplayMode } from '@/hooks/useNameDisplayMode'
import { formatBytes } from '@/utils/format'

// Reactive dark-mode detector — mirrors the run-detail charts so the
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

// ECharts inserts the tooltip string as HTML, so anything we pulled from
// a raw test name needs to be escaped before it lands in the markup.
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

type ChartOrder = 'index' | 'size'

interface PayloadSizesSectionProps {
  tests: SuiteTest[]
  /** Called when a bar is clicked, with the 1-based test index (matches the `#` column in the tests table). */
  onTestClick?: (index: number) => void
  /**
   * External search query that filters which tests appear in the chart.
   * Controlled by the parent so the same query also drives the tests
   * table and other sections on the suite-details page.
   */
  searchQuery?: string
}

interface PayloadRow {
  index: number // 1-based position in the suite, matches the # column in the tests table.
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
function toRow(t: SuiteTest, index: number): PayloadRow | null {
  const testStep = t.payload_sizes?.test
  if (!testStep) return null
  const u = sumArray(testStep.ssz_full)
  const b = sumArray(testStep.ssz_bal)
  const s = sumArray(testStep.ssz_full_snappy)
  if (u === 0 && b === 0 && s === 0) return null
  return { index, name: t.name, uncompressed: u, bal: b, snappy: s }
}

export function PayloadSizesSection({ tests, onTestClick, searchQuery = '' }: PayloadSizesSectionProps) {
  const [order, setOrder] = useState<ChartOrder>('index')
  const isDark = useDarkMode()
  const { mode: nameMode } = useNameDisplayMode()
  const chartRef = useRef<ReactECharts | null>(null)

  const rows = useMemo(() => {
    const all: PayloadRow[] = []
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

  // Optional size-descending sort. `index` mode keeps the original
  // suite order, which makes the X axis monotonic (#1, #2, #3, …) and
  // useful for scanning patterns across the suite. `size` mode shows
  // the heaviest payloads first.
  const ordered = useMemo(
    () => order === 'size' ? [...filtered].sort((a, b) => b.uncompressed - a.uncompressed) : filtered,
    [filtered, order],
  )

  // Looking up names by category lets the tooltip stay meaningful even
  // when the chart is sorted by size and the X axis is no longer monotonic.
  const categories = useMemo(() => ordered.map((r) => String(r.index)), [ordered])
  const indexToName = useMemo(
    () => new Map(ordered.map((r) => [String(r.index), r.name])),
    [ordered],
  )

  // Click-anywhere-in-a-column → open the test modal. The default ECharts
  // `click` event only fires when the cursor actually hits a bar, leaving
  // dead zones above short bars and in the gaps between stacks. Hooking
  // the underlying Zrender canvas and converting the click's pixel back
  // to an X-axis category gives us the column the user "meant" wherever
  // they click inside the plot area.
  useEffect(() => {
    if (!onTestClick) return
    const inst = chartRef.current?.getEchartsInstance()
    if (!inst) return
    const zr = inst.getZr()
    const handler = (event: { offsetX: number; offsetY: number }) => {
      const pixel: [number, number] = [event.offsetX, event.offsetY]
      // Only fire when the click is over the grid (plot area), not the
      // axis labels, legend, or data-zoom slider.
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

  const maxUncompressed = Math.max(...rows.map((r) => r.uncompressed))

  // Show the full range by default; the user can drag the dataZoom slider
  // to narrow in on a slice if they want.
  const initialZoomEnd = 100

  return (
    <>
      <div className="flex w-full items-center gap-2 border-b border-gray-200 px-4 py-3 text-sm/6 font-medium text-gray-900 dark:border-gray-700 dark:text-gray-100">
        <BarChart3 className="size-4 text-gray-400 dark:text-gray-500" />
        Payload Sizes
        <span className="text-xs/5 text-gray-500 dark:text-gray-400">
          · {rows.length} tests · max {formatBytes(maxUncompressed)}
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
              { value: 'size', label: 'Size' },
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
            // Theme-aware colors matching the resource-usage charts on the
            // run-detail page — grey-on-grey was unreadable in dark mode.
            const textColor = isDark ? '#ffffff' : '#374151'
            const mutedTextColor = isDark ? '#9ca3af' : '#6b7280'
            const axisLineColor = isDark ? '#4b5563' : '#d1d5db'
            const splitLineColor = isDark ? '#374151' : '#e5e7eb'
            return {
              backgroundColor: 'transparent',
              textStyle: { color: textColor },
              grid: { left: 70, right: 24, top: 32, bottom: 60, containLabel: false },
              tooltip: {
                trigger: 'axis',
                axisPointer: { type: 'shadow' },
                backgroundColor: isDark ? '#1f2937' : '#ffffff',
                borderColor: isDark ? '#374151' : '#e5e7eb',
                textStyle: { color: textColor },
                // `confine` keeps the popover inside the chart container; the
                // CSS clamps width and lets the decomposed line wrap.
                confine: true,
                extraCssText: 'max-width: 300px; white-space: normal;',
                formatter: (params: Array<{ seriesName: string; color: string; data: number; name: string }>) => {
                  if (!params.length) return ''
                  const idx = params[0].name ?? ''
                  const rawName = indexToName.get(idx) ?? ''
                  const decomposedSafe = escapeHtml(formatTestNameLong(rawName, nameMode))
                  let content = `<strong>Test #${escapeHtml(idx)}</strong>`
                  content += `<br/><span style="font-size:11px;color:${mutedTextColor};word-break:break-all;display:block;">${decomposedSafe}</span><br/>`
                  for (const p of params) {
                    content += `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${p.color};margin-right:6px;"></span>${escapeHtml(p.seriesName)}: ${formatBytes(p.data)}<br/>`
                  }
                  return content
                },
              },
              legend: {
                top: 0,
                data: ['Non-BAL', 'BAL', 'Snappy'],
                textStyle: { color: textColor },
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
                axisLabel: { color: textColor, formatter: (v: number) => formatBytes(v) },
                axisLine: { show: true, lineStyle: { color: axisLineColor } },
                splitLine: { lineStyle: { color: splitLineColor } },
              },
              dataZoom: [
                {
                  type: 'slider',
                  xAxisIndex: 0,
                  start: 0,
                  end: initialZoomEnd,
                  height: 14,
                  bottom: 8,
                  borderColor: axisLineColor,
                  fillerColor: isDark ? 'rgba(139, 92, 246, 0.3)' : 'rgba(139, 92, 246, 0.1)',
                  backgroundColor: isDark ? '#374151' : '#f3f4f6',
                  textStyle: { color: textColor },
                },
                { type: 'inside', xAxisIndex: 0, start: 0, end: initialZoomEnd },
              ],
              series: [
                {
                  name: 'Non-BAL',
                  type: 'bar',
                  stack: 'uncompressed',
                  itemStyle: { color: '#3b82f6' },
                  data: ordered.map((r) => Math.max(0, r.uncompressed - r.bal)),
                },
                {
                  name: 'BAL',
                  type: 'bar',
                  stack: 'uncompressed',
                  itemStyle: { color: '#f59e0b' },
                  data: ordered.map((r) => r.bal),
                },
                {
                  name: 'Snappy',
                  type: 'bar',
                  itemStyle: { color: '#10b981' },
                  data: ordered.map((r) => r.snappy),
                },
              ],
            }
          })()}
          style={{ height: 360 }}
          notMerge
        />
      </div>
    </>
  )
}
