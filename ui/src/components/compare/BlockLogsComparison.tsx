import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import { Blocks } from 'lucide-react'
import type { BlockLogs, SuiteTest } from '@/api/types'
import { type CompareRun, type LabelMode, RUN_SLOTS, formatRunLabel } from './constants'

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

interface BlockLogDataPoint {
  testIndex: number
  testName: string
  throughput: number
  executionMs: number
  overheadMs: number
  accountCacheHitRate: number
  storageCacheHitRate: number
  codeCacheHitRate: number
}

/** Build a unified, sorted list of test names across all runs. */
function buildUnifiedTestList(
  blockLogsPerRun: (BlockLogs | null)[],
  suiteTests?: SuiteTest[],
  nameFilter?: (name: string) => boolean,
): string[] {
  const allNames = new Set<string>()
  for (const bl of blockLogsPerRun) {
    if (bl) for (const name of Object.keys(bl)) {
      if (!nameFilter || nameFilter(name)) allNames.add(name)
    }
  }

  const suiteOrder = new Map<string, number>()
  if (suiteTests) {
    suiteTests.forEach((t, i) => suiteOrder.set(t.name, i))
  }

  return [...allNames].sort((a, b) => {
    const orderA = suiteOrder.get(a)
    const orderB = suiteOrder.get(b)
    if (orderA !== undefined && orderB !== undefined) return orderA - orderB
    if (orderA !== undefined) return -1
    if (orderB !== undefined) return 1
    return a.localeCompare(b)
  })
}

/** Map a single run's block logs onto the unified test list indices. Missing tests produce no entry. */
function buildBlockLogDataPoints(
  blockLogs: BlockLogs,
  unifiedTests: string[],
): BlockLogDataPoint[] {
  const points: BlockLogDataPoint[] = []
  unifiedTests.forEach((testName, index) => {
    const entry = blockLogs[testName]
    if (!entry) return
    points.push({
      testIndex: index + 1,
      testName,
      throughput: entry.throughput?.mgas_per_sec ?? 0,
      executionMs: entry.timing?.execution_ms ?? 0,
      overheadMs: (entry.timing?.state_read_ms ?? 0) + (entry.timing?.state_hash_ms ?? 0) + (entry.timing?.commit_ms ?? 0),
      accountCacheHitRate: entry.cache?.account?.hit_rate ?? 0,
      storageCacheHitRate: entry.cache?.storage?.hit_rate ?? 0,
      codeCacheHitRate: entry.cache?.code?.hit_rate ?? 0,
    })
  })
  return points
}

interface ChartSectionProps {
  title: string
  option: object
  onZoom: (start: number, end: number) => void
}

function ChartSection({ title, option, onZoom }: ChartSectionProps) {
  const onEvents = useMemo(
    () => ({
      datazoom: (params: { start?: number; end?: number; batch?: Array<{ start: number; end: number }> }) => {
        if (params.batch && params.batch.length > 0) {
          onZoom(params.batch[0].start, params.batch[0].end)
        } else if (params.start !== undefined && params.end !== undefined) {
          onZoom(params.start, params.end)
        }
      },
    }),
    [onZoom],
  )

  return (
    <div className="rounded-xs bg-gray-50 p-3 dark:bg-gray-700/50">
      <h4 className="mb-2 text-xs font-medium text-gray-700 dark:text-gray-300">{title}</h4>
      <ReactECharts
        option={option}
        style={{ height: '200px', width: '100%' }}
        opts={{ renderer: 'svg' }}
        onEvents={onEvents}
      />
    </div>
  )
}

interface BlockLogsComparisonProps {
  runs: CompareRun[]
  blockLogsPerRun: (BlockLogs | null)[]
  blockLogsLoading: boolean
  suiteTests?: SuiteTest[]
  labelMode: LabelMode
  testNameFilter?: (name: string) => boolean
}

export function BlockLogsComparison({ runs, blockLogsPerRun, blockLogsLoading, suiteTests, labelMode, testNameFilter }: BlockLogsComparisonProps) {
  const isDark = useDarkMode()
  const [zoomRange, setZoomRange] = useState({ start: 0, end: 100 })
  const prevZoomRef = useRef(zoomRange)

  const handleZoom = useCallback((start: number, end: number) => {
    if (prevZoomRef.current.start !== start || prevZoomRef.current.end !== end) {
      prevZoomRef.current = { start, end }
      setZoomRange({ start, end })
    }
  }, [])

  const unifiedTests = useMemo(
    () => buildUnifiedTestList(blockLogsPerRun, suiteTests, testNameFilter),
    [blockLogsPerRun, suiteTests, testNameFilter],
  )

  const pointsPerRun = useMemo(
    () => blockLogsPerRun.map((bl) => (bl ? buildBlockLogDataPoints(bl, unifiedTests) : [])),
    [blockLogsPerRun, unifiedTests],
  )

  const hasAnyData = blockLogsPerRun.some((bl) => bl !== null)
  const runsWithData = runs.filter((_, i) => blockLogsPerRun[i] !== null)
  const runsWithoutData = runs.filter((_, i) => blockLogsPerRun[i] === null)

  const chartOptions = useMemo(() => {
    const textColor = isDark ? '#ffffff' : '#374151'
    const axisLineColor = isDark ? '#4b5563' : '#d1d5db'
    const splitLineColor = isDark ? '#374151' : '#e5e7eb'
    const maxLen = unifiedTests.length
    const suiteOrder = new Map<string, number>()
    if (suiteTests) {
      suiteTests.forEach((t, i) => suiteOrder.set(t.name, i + 1))
    }
    const indexToOrder = new Map<number, number>()
    unifiedTests.forEach((name, i) => {
      indexToOrder.set(i + 1, suiteOrder.get(name) ?? (i + 1))
    })

    const baseConfig = {
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
      xAxis: {
        type: 'value' as const,
        min: 1,
        max: maxLen || 1,
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
    }

    const createLineSeries = () => ({
      type: 'line' as const,
      smooth: maxLen <= 100,
      showSymbol: maxLen <= 100,
      symbolSize: 4,
      lineStyle: { width: 2 },
    })

    const clientBySeriesName = new Map<string, string>()
    for (let i = 0; i < runs.length; i++) {
      const client = runs[i].config.instance.client
      const label = RUN_SLOTS[i].label
      clientBySeriesName.set(`Run ${label}`, client)
    }

    const createTooltip = (formatter: (value: number) => string) => ({
      trigger: 'axis' as const,
      appendToBody: true,
      backgroundColor: isDark ? '#1f2937' : '#ffffff',
      borderColor: isDark ? '#374151' : '#e5e7eb',
      textStyle: { color: textColor },
      extraCssText: 'max-width: 300px; white-space: normal;',
      formatter: (
        params: Array<{ seriesName: string; color: string; value: [number, number | null, string, number] }>,
      ) => {
        const visible = params.filter((p) => p.value[1] != null)
        if (!visible.length) return ''
        const testName = visible[0].value[2]
        const testOrder = visible[0].value[3]
        let content = `<strong>Test #${testOrder}</strong>`
        if (testName) content += `<br/><span style="font-size: 10px; color: ${isDark ? '#9ca3af' : '#6b7280'};">${testName}</span>`
        content += '<br/>'
        visible.forEach((p) => {
          const value = p.value[1] as number
          const client = clientBySeriesName.get(p.seriesName)
          const clientImg = client ? `<img src="/img/clients/${client}.jpg" style="display:inline-block;width:14px;height:14px;border-radius:50%;object-fit:cover;vertical-align:middle;margin-right:4px;" />` : ''
          content += `${clientImg}<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${p.color};margin-right:6px;vertical-align:middle;"></span>${p.seriesName}: ${formatter(value)}<br/>`
        })
        return content
      },
    })

    const createYAxis = (formatter: (value: number) => string) => ({
      type: 'value' as const,
      axisLabel: { color: textColor, fontSize: 11, formatter },
      axisLine: { show: true, lineStyle: { color: axisLineColor } },
      axisTick: { show: true, lineStyle: { color: axisLineColor } },
      splitLine: { lineStyle: { color: splitLineColor } },
    })

    const buildSimpleSeries = (field: keyof BlockLogDataPoint) =>
      runsWithData.map((run) => {
        const slot = RUN_SLOTS[run.index]
        const pointsByIndex = new Map(pointsPerRun[run.index].map((d) => [d.testIndex, d]))
        return {
          name: `Run ${formatRunLabel(slot, run, labelMode)}`,
          ...createLineSeries(),
          connectNulls: false,
          data: unifiedTests.map((testName, i) => {
            const d = pointsByIndex.get(i + 1)
            return [i + 1, d ? d[field] : null, testName, indexToOrder.get(i + 1) ?? (i + 1)]
          }),
          itemStyle: { color: slot.color },
          areaStyle: { opacity: 0.08, color: slot.color },
        }
      })

    const buildChart = (field: keyof BlockLogDataPoint, yFmt: (v: number) => string, tipFmt: (v: number) => string) => ({
      ...baseConfig,
      tooltip: createTooltip(tipFmt),
      yAxis: createYAxis(yFmt),
      series: buildSimpleSeries(field),
    })

    return {
      throughput: buildChart('throughput', (v) => `${v.toFixed(0)}`, (v) => `${v.toFixed(2)} MGas/s`),
      executionMs: buildChart('executionMs', (v) => `${v.toFixed(0)} ms`, (v) => `${v.toFixed(2)} ms`),
      overheadMs: buildChart('overheadMs', (v) => `${v.toFixed(0)} ms`, (v) => `${v.toFixed(2)} ms`),
      accountCacheHitRate: buildChart('accountCacheHitRate', (v) => `${v.toFixed(0)}%`, (v) => `${v.toFixed(1)}%`),
      storageCacheHitRate: buildChart('storageCacheHitRate', (v) => `${v.toFixed(0)}%`, (v) => `${v.toFixed(1)}%`),
      codeCacheHitRate: buildChart('codeCacheHitRate', (v) => `${v.toFixed(0)}%`, (v) => `${v.toFixed(1)}%`),
    }
  }, [pointsPerRun, runs, runsWithData, isDark, zoomRange, unifiedTests, labelMode, suiteTests])

  if (blockLogsLoading) {
    return (
      <div className="rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
        <div className="flex items-center justify-center py-8 text-sm/6 text-gray-500 dark:text-gray-400">
          Loading block logs...
        </div>
      </div>
    )
  }

  if (!hasAnyData) return null

  return (
    <div className="rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
      <div className="mb-4 flex items-center gap-2">
        <Blocks className="size-4 text-gray-400 dark:text-gray-500" />
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Block Logs Comparison</h3>
        <div className="ml-auto flex items-center gap-2 text-xs/5">
          {runsWithData.map((run) => {
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

      {runsWithoutData.length > 0 && (
        <p className="mb-3 text-xs/5 text-gray-500 dark:text-gray-400">
          Block logs unavailable for run{runsWithoutData.length > 1 ? 's' : ''}{' '}
          {runsWithoutData.map((r) => RUN_SLOTS[r.index].label).join(', ')}
        </p>
      )}

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <ChartSection title="Throughput (MGas/s)" option={chartOptions.throughput} onZoom={handleZoom} />
        <ChartSection title="Execution Time (ms)" option={chartOptions.executionMs} onZoom={handleZoom} />
        <ChartSection title="Overhead — State Read/Hash/Commit (ms)" option={chartOptions.overheadMs} onZoom={handleZoom} />
        <ChartSection title="Account Cache Hit Rate (%)" option={chartOptions.accountCacheHitRate} onZoom={handleZoom} />
        <ChartSection title="Storage Cache Hit Rate (%)" option={chartOptions.storageCacheHitRate} onZoom={handleZoom} />
        <ChartSection title="Code Cache Hit Rate (%)" option={chartOptions.codeCacheHitRate} onZoom={handleZoom} />
      </div>

      <p className="mt-4 text-center text-xs/5 text-gray-500 dark:text-gray-400">
        Block log metrics per test (ordered by execution) - drag slider to zoom
      </p>
    </div>
  )
}
