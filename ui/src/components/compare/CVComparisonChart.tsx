import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import { Activity } from 'lucide-react'
import type { RunResult, SuiteTest } from '@/api/types'
import { type ChartType, type CompareRun, type LabelMode, RUN_SLOTS, formatRunLabel } from './constants'
import type { ZoomRange } from './MGasComparisonChart'

interface CVComparisonChartProps {
  runs: CompareRun[]
  suiteTests?: SuiteTest[]
  labelMode: LabelMode
  testNameFilter?: (name: string) => boolean
  zoomRange?: ZoomRange
  onZoomChange?: (range: ZoomRange) => void
  chartType?: ChartType
  /** Per-run variance keyed by CompareRun.index. */
  varianceByRunIndex: Map<number, Record<string, { mgasStddev: number; mgasMean: number }>>
}

function useDarkMode() {
  const [isDark, setIsDark] = useState(() => document.documentElement.classList.contains('dark'))

  useEffect(() => {
    const observer = new MutationObserver(() => {
      setIsDark(document.documentElement.classList.contains('dark'))
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [])

  return isDark
}

interface CVDataPoint {
  testIndex: number
  testOrder: number
  testName: string
  cv: number
}

function buildCVData(
  result: RunResult,
  suiteTests: SuiteTest[] | undefined,
  variance: Record<string, { mgasStddev: number; mgasMean: number }> | undefined,
  nameFilter?: (name: string) => boolean,
): CVDataPoint[] {
  if (!variance) return []

  const suiteOrder = new Map<string, number>()
  if (suiteTests) {
    suiteTests.forEach((t, i) => suiteOrder.set(t.name, i + 1))
  }

  const entries: { name: string; order: number; cv: number }[] = []
  for (const [name, entry] of Object.entries(result.tests)) {
    if (nameFilter && !nameFilter(name)) continue
    const v = variance[name]
    if (!v || v.mgasMean <= 0) continue
    const cv = (v.mgasStddev / v.mgasMean) * 100
    const order = suiteOrder.get(name) ?? (parseInt(entry.dir, 10) || 0)
    entries.push({ name, order, cv })
  }

  entries.sort((a, b) => a.order - b.order)
  return entries.map((e, i) => ({ testIndex: i + 1, testOrder: e.order, testName: e.name, cv: e.cv }))
}

export function CVComparisonChart({ runs, suiteTests, labelMode, testNameFilter, zoomRange: externalZoom, onZoomChange, chartType = 'line', varianceByRunIndex }: CVComparisonChartProps) {
  const isDark = useDarkMode()
  const [internalZoom, setInternalZoom] = useState({ start: 0, end: 100 })
  const zoomRange = externalZoom ?? internalZoom
  const prevZoomRef = useRef(zoomRange)
  const [threshold, setThreshold] = useState(20)

  const handleZoom = useCallback((params: { start?: number; end?: number; batch?: Array<{ start: number; end: number }> }) => {
    let start: number | undefined
    let end: number | undefined
    if (params.batch && params.batch.length > 0) {
      start = params.batch[0].start
      end = params.batch[0].end
    } else {
      start = params.start
      end = params.end
    }
    if (start !== undefined && end !== undefined && (prevZoomRef.current.start !== start || prevZoomRef.current.end !== end)) {
      const newRange = { start, end }
      prevZoomRef.current = newRange
      setInternalZoom(newRange)
      onZoomChange?.(newRange)
    }
  }, [onZoomChange])

  const onEvents = useMemo(() => ({ datazoom: handleZoom }), [handleZoom])

  const pointsPerRun = useMemo(
    () => runs.map((r) => r.result ? buildCVData(r.result, suiteTests, varianceByRunIndex.get(r.index), testNameFilter) : []),
    [runs, suiteTests, varianceByRunIndex, testNameFilter],
  )

  const option = useMemo(() => {
    const textColor = isDark ? '#ffffff' : '#374151'
    const axisLineColor = isDark ? '#4b5563' : '#d1d5db'
    const splitLineColor = isDark ? '#374151' : '#e5e7eb'
    const maxLen = Math.max(...pointsPerRun.map((p) => p.length))
    const indexToOrder = new Map<number, number>()
    for (const points of pointsPerRun) {
      for (const d of points) {
        indexToOrder.set(d.testIndex, d.testOrder)
      }
    }
    const clientBySeriesName = new Map(runs.map((r, i) => [`Run ${formatRunLabel(RUN_SLOTS[i], r, labelMode)}`, r.config.instance.client]))

    return {
      backgroundColor: 'transparent',
      animation: maxLen <= 100,
      textStyle: { color: textColor },
      grid: {
        left: '3%',
        right: '4%',
        bottom: '50',
        top: '15%',
        containLabel: true,
      },
      tooltip: {
        trigger: 'axis' as const,
        appendToBody: true,
        backgroundColor: isDark ? '#1f2937' : '#ffffff',
        borderColor: isDark ? '#374151' : '#e5e7eb',
        textStyle: { color: textColor },
        extraCssText: 'max-width: 300px; white-space: normal;',
        formatter: (
          params: Array<{ seriesName: string; color: string; value: [number, number, string, number] }>,
        ) => {
          if (!params.length) return ''
          const testOrder = params[0].value[3]
          const testName = params[0].value[2]
          let content = `<strong>Test #${testOrder}</strong>`
          if (testName) content += `<br/><span style="font-size: 10px; color: ${isDark ? '#9ca3af' : '#6b7280'};">${testName}</span>`
          content += '<br/>'
          params.forEach((p) => {
            const value = p.value[1]
            const client = clientBySeriesName.get(p.seriesName)
            const clientImg = client ? `<img src="/img/clients/${client}.jpg" style="display:inline-block;width:14px;height:14px;border-radius:50%;object-fit:cover;vertical-align:middle;margin-right:4px;" />` : ''
            content += `${clientImg}<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${p.color};margin-right:6px;vertical-align:middle;"></span>${p.seriesName}: ${value.toFixed(2)}%<br/>`
          })
          return content
        },
      },
      xAxis: {
        type: 'value' as const,
        min: 1,
        max: maxLen,
        minInterval: 1,
        axisLabel: {
          color: textColor,
          fontSize: 11,
          formatter: (value: number) => `#${indexToOrder.get(value) ?? value}`,
        },
        axisLine: { show: true, lineStyle: { color: axisLineColor } },
        axisTick: { show: true, lineStyle: { color: axisLineColor } },
        splitLine: { show: false },
      },
      yAxis: {
        type: 'value' as const,
        axisLabel: {
          color: textColor,
          fontSize: 11,
          formatter: (value: number) => `${value.toFixed(1)}%`,
        },
        axisLine: { show: true, lineStyle: { color: axisLineColor } },
        axisTick: { show: true, lineStyle: { color: axisLineColor } },
        splitLine: { lineStyle: { color: splitLineColor } },
        name: 'CV (%)',
        nameTextStyle: { color: textColor, fontSize: 11 },
      },
      legend: {
        bottom: 25,
        textStyle: { color: textColor, fontSize: 11 },
        itemWidth: 12,
        itemHeight: 8,
      },
      dataZoom: [
        {
          type: 'slider' as const,
          xAxisIndex: 0,
          start: zoomRange.start,
          end: zoomRange.end,
          height: 20,
          bottom: 5,
          borderColor: axisLineColor,
          fillerColor: isDark ? 'rgba(139, 92, 246, 0.3)' : 'rgba(139, 92, 246, 0.1)',
          backgroundColor: isDark ? '#374151' : '#f3f4f6',
          textStyle: { color: textColor },
          labelFormatter: (value: number) => `#${indexToOrder.get(Math.round(value)) ?? Math.round(value)}`,
        },
        {
          type: 'inside' as const,
          xAxisIndex: 0,
          start: zoomRange.start,
          end: zoomRange.end,
          zoomOnMouseWheel: true,
          moveOnMouseMove: true,
          moveOnMouseWheel: false,
        },
      ],
      series: runs.map((_run, i) => {
        const slot = RUN_SLOTS[i]
        const points = pointsPerRun[i]
        const data = points.map((d) => [d.testIndex, d.cv, d.testName, d.testOrder])
        const markLine = i === 0
          ? {
              silent: true,
              symbol: 'none' as const,
              lineStyle: {
                color: isDark ? '#ef4444' : '#dc2626',
                type: 'dashed' as const,
                width: 1.5,
              },
              label: {
                show: true,
                position: 'insideEndTop' as const,
                formatter: `${threshold}%`,
                color: isDark ? '#fca5a5' : '#b91c1c',
                fontSize: 10,
              },
              data: [{ yAxis: threshold }],
            }
          : undefined
        const base = {
          name: `Run ${formatRunLabel(slot, runs[i], labelMode)}`,
          data,
          itemStyle: { color: slot.color },
          ...(markLine ? { markLine } : {}),
        }
        if (chartType === 'bar') {
          return { ...base, type: 'bar' as const, barMaxWidth: 6 }
        }
        if (chartType === 'dot') {
          return { ...base, type: 'scatter' as const, symbolSize: 4 }
        }
        return {
          ...base,
          type: 'line' as const,
          smooth: maxLen <= 100,
          showSymbol: maxLen <= 100,
          symbolSize: 4,
          lineStyle: { width: 2 },
          areaStyle: { opacity: 0.08, color: slot.color },
        }
      }),
    }
  }, [pointsPerRun, runs, isDark, zoomRange, labelMode, chartType, threshold])

  if (pointsPerRun.every((p) => p.length === 0)) return null

  return (
    <div className="rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
      <div className="mb-2 flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2">
          <Activity className="size-4 text-gray-400 dark:text-gray-500" />
          <h3
            className="text-sm/6 font-medium text-gray-900 dark:text-gray-100"
            title="Coefficient of Variation — standard deviation of MGas/s as a percentage of the mean, across the sampled runs in each group. Lower = more consistent."
          >
            Coefficient of Variation per Test (MGas/s)
          </h3>
        </div>
        <div className="flex flex-wrap items-center gap-3 text-xs/5">
          <label className="flex items-center gap-1.5 text-gray-500 dark:text-gray-400">
            <span>Threshold:</span>
            <input
              type="range"
              min={0}
              max={50}
              step={1}
              value={threshold}
              onChange={(e) => setThreshold(Number(e.target.value))}
              className="w-24 accent-red-600 dark:accent-red-500"
            />
            <span className="w-8 font-mono tabular-nums text-gray-700 dark:text-gray-300">{threshold}%</span>
          </label>
          {runs.map((run) => {
            const slot = RUN_SLOTS[run.index]
            return (
              <span key={slot.label} className={`inline-flex items-center gap-1.5 rounded-sm px-2 py-0.5 font-medium ${slot.badgeBgClass} ${slot.badgeTextClass}`}>
                <img src={`/img/clients/${run.config.instance.client}.jpg`} alt={run.config.instance.client} className="size-3.5 rounded-full object-cover" />
                {formatRunLabel(slot, run, labelMode)}
              </span>
            )
          })}
        </div>
      </div>
      <ReactECharts
        option={option}
        style={{ height: '300px', width: '100%' }}
        opts={{ renderer: 'svg' }}
        onEvents={onEvents}
      />
      <div className="mt-3 border-t border-gray-200 pt-3 dark:border-gray-700">
        <table className="w-full text-xs/5">
          <thead>
            <tr className="text-gray-500 dark:text-gray-400">
              <th className="pb-1 text-left font-medium">Run</th>
              <th className="pb-1 text-right font-medium" title={`Tests with CV ≤ ${threshold}%`}>Below</th>
              <th className="pb-1 text-right font-medium">Avg</th>
              <th className="pb-1 text-right font-medium">P95</th>
              <th className="pb-1 pr-3 text-right font-medium">Max</th>
              <th className="border-l border-gray-200 pb-1 pl-3 text-right font-medium dark:border-gray-600" title={`Tests with CV > ${threshold}%`}>Above</th>
              <th className="pb-1 text-right font-medium">Avg</th>
              <th className="pb-1 text-right font-medium">P95</th>
              <th className="pb-1 pr-3 text-right font-medium">Max</th>
              <th className="border-l border-gray-200 pb-1 pl-3 text-right font-medium dark:border-gray-600">Total</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
            {runs.map((run, i) => {
              const slot = RUN_SLOTS[run.index]
              const points = pointsPerRun[i]
              const below: number[] = []
              const above: number[] = []
              for (const p of points) {
                if (p.cv > threshold) above.push(p.cv)
                else below.push(p.cv)
              }
              const total = below.length + above.length
              const avg = (arr: number[]) => arr.length > 0 ? arr.reduce((a, b) => a + b, 0) / arr.length : 0
              const percentile = (arr: number[], p: number) => {
                if (arr.length === 0) return 0
                const sorted = [...arr].sort((a, b) => a - b)
                const idx = Math.ceil((p / 100) * sorted.length) - 1
                return sorted[Math.max(0, idx)]
              }
              const max = (arr: number[]) => arr.length > 0 ? Math.max(...arr) : 0
              return (
                <tr key={slot.label}>
                  <td className="py-1">
                    <span className="inline-flex items-center gap-1.5 font-medium" style={{ color: slot.color }}>
                      <img src={`/img/clients/${run.config.instance.client}.jpg`} alt={run.config.instance.client} className="size-3.5 rounded-full object-cover" />
                      {formatRunLabel(slot, run, labelMode)}
                    </span>
                  </td>
                  <td className="py-1 text-right font-medium text-green-600 dark:text-green-400">{below.length}</td>
                  <td className="py-1 text-right text-green-600 dark:text-green-400">{below.length > 0 ? `${avg(below).toFixed(1)}%` : '-'}</td>
                  <td className="py-1 text-right text-green-600 dark:text-green-400">{below.length > 0 ? `${percentile(below, 95).toFixed(1)}%` : '-'}</td>
                  <td className="py-1 pr-3 text-right text-green-600 dark:text-green-400">{below.length > 0 ? `${max(below).toFixed(1)}%` : '-'}</td>
                  <td className="border-l border-gray-200 py-1 pl-3 text-right font-medium text-red-600 dark:border-gray-600 dark:text-red-400">{above.length}</td>
                  <td className="py-1 text-right text-red-600 dark:text-red-400">{above.length > 0 ? `${avg(above).toFixed(1)}%` : '-'}</td>
                  <td className="py-1 text-right text-red-600 dark:text-red-400">{above.length > 0 ? `${percentile(above, 95).toFixed(1)}%` : '-'}</td>
                  <td className="py-1 pr-3 text-right text-red-600 dark:text-red-400">{above.length > 0 ? `${max(above).toFixed(1)}%` : '-'}</td>
                  <td className="border-l border-gray-200 py-1 pl-3 text-right text-gray-500 dark:border-gray-600 dark:text-gray-400">{total}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
