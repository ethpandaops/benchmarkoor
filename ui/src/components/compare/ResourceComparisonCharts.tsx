import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import { Cpu } from 'lucide-react'
import type { TestEntry, ResourceTotals } from '@/api/types'
import { formatBytes } from '@/utils/format'

interface AggregatedResourceData {
  totals: ResourceTotals
  timeTotalNs: number
  memoryBytes: number
}

function getAggregatedResourceData(entry: TestEntry): AggregatedResourceData | undefined {
  if (!entry.steps) return undefined

  const steps = [entry.steps.setup, entry.steps.test, entry.steps.cleanup].filter((s) => s?.aggregated?.resource_totals)

  if (steps.length === 0) return undefined

  let cpuUsec = 0
  let memoryDelta = 0
  let diskRead = 0
  let diskWrite = 0
  let diskReadOps = 0
  let diskWriteOps = 0
  let timeTotalNs = 0
  let memoryBytes = 0

  for (const step of steps) {
    if (step?.aggregated) {
      timeTotalNs += step.aggregated.time_total ?? 0
      if (step.aggregated.resource_totals) {
        const res = step.aggregated.resource_totals
        cpuUsec += res.cpu_usec ?? 0
        memoryDelta += res.memory_delta_bytes ?? 0
        diskRead += res.disk_read_bytes ?? 0
        diskWrite += res.disk_write_bytes ?? 0
        diskReadOps += res.disk_read_iops ?? 0
        diskWriteOps += res.disk_write_iops ?? 0
        const stepMemory = res.memory_bytes ?? 0
        if (stepMemory > memoryBytes) memoryBytes = stepMemory
      }
    }
  }

  return {
    totals: {
      cpu_usec: cpuUsec,
      memory_delta_bytes: memoryDelta,
      memory_bytes: memoryBytes,
      disk_read_bytes: diskRead,
      disk_write_bytes: diskWrite,
      disk_read_iops: diskReadOps,
      disk_write_iops: diskWriteOps,
    },
    timeTotalNs,
    memoryBytes,
  }
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

interface ResourceComparisonChartsProps {
  testsA: Record<string, TestEntry>
  testsB: Record<string, TestEntry>
}

interface ResourceDataPoint {
  testIndex: number
  testName: string
  cpuPercent: number
  memoryMB: number
  cpuUsec: number
  memoryDelta: number
  diskRead: number
  diskWrite: number
  diskReadOps: number
  diskWriteOps: number
}

function formatMicroseconds(usec: number): string {
  if (usec < 1000) return `${usec.toFixed(0)} \u00b5s`
  if (usec < 1_000_000) return `${(usec / 1000).toFixed(1)} ms`
  return `${(usec / 1_000_000).toFixed(2)} s`
}

function formatOps(ops: number): string {
  if (ops < 1000) return `${ops.toFixed(0)}`
  if (ops < 1_000_000) return `${(ops / 1000).toFixed(1)}K`
  return `${(ops / 1_000_000).toFixed(1)}M`
}

function buildDataPoints(tests: Record<string, TestEntry>): ResourceDataPoint[] {
  const sortedTests = Object.entries(tests).sort(([, a], [, b]) => {
    const aNum = parseInt(a.dir, 10) || 0
    const bNum = parseInt(b.dir, 10) || 0
    return aNum - bNum
  })

  const points: ResourceDataPoint[] = []
  sortedTests.forEach(([testName, test], index) => {
    const agg = getAggregatedResourceData(test)
    if (agg) {
      const res = agg.totals
      let cpuPercent = 0
      if (agg.timeTotalNs > 0) {
        cpuPercent = ((res.cpu_usec ?? 0) / (agg.timeTotalNs / 1000)) * 100
      }
      points.push({
        testIndex: index + 1,
        testName,
        cpuPercent,
        memoryMB: agg.memoryBytes / (1024 * 1024),
        cpuUsec: res.cpu_usec ?? 0,
        memoryDelta: res.memory_delta_bytes ?? 0,
        diskRead: res.disk_read_bytes ?? 0,
        diskWrite: res.disk_write_bytes ?? 0,
        diskReadOps: res.disk_read_iops ?? 0,
        diskWriteOps: res.disk_write_iops ?? 0,
      })
    }
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

export function ResourceComparisonCharts({ testsA, testsB }: ResourceComparisonChartsProps) {
  const isDark = useDarkMode()
  const [zoomRange, setZoomRange] = useState({ start: 0, end: 100 })
  const prevZoomRef = useRef(zoomRange)

  const handleZoom = useCallback((start: number, end: number) => {
    if (prevZoomRef.current.start !== start || prevZoomRef.current.end !== end) {
      prevZoomRef.current = { start, end }
      setZoomRange({ start, end })
    }
  }, [])

  const pointsA = useMemo(() => buildDataPoints(testsA), [testsA])
  const pointsB = useMemo(() => buildDataPoints(testsB), [testsB])

  const hasData = pointsA.length > 0 || pointsB.length > 0

  const chartOptions = useMemo(() => {
    const textColor = isDark ? '#ffffff' : '#374151'
    const axisLineColor = isDark ? '#4b5563' : '#d1d5db'
    const splitLineColor = isDark ? '#374151' : '#e5e7eb'
    const maxLen = Math.max(pointsA.length, pointsB.length)

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
        max: maxLen,
        minInterval: 1,
        axisLabel: {
          color: textColor,
          fontSize: 11,
          formatter: (value: number) => `#${value}`,
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
          labelFormatter: (value: number) => `#${Math.round(value)}`,
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

    const lineA = {
      type: 'line' as const,
      smooth: maxLen <= 100,
      showSymbol: maxLen <= 100,
      symbolSize: 4,
      lineStyle: { width: 2 },
    }
    const lineB = { ...lineA }

    const createTooltip = (formatter: (value: number) => string) => ({
      trigger: 'axis' as const,
      backgroundColor: isDark ? '#1f2937' : '#ffffff',
      borderColor: isDark ? '#374151' : '#e5e7eb',
      textStyle: { color: textColor },
      extraCssText: 'max-width: 300px; white-space: normal;',
      formatter: (
        params: Array<{ seriesName: string; color: string; value: [number, number, string] }>,
      ) => {
        if (!params.length) return ''
        const testIndex = params[0].value[0]
        let content = `<strong>Test #${testIndex}</strong><br/>`
        params.forEach((p) => {
          const value = p.value[1]
          const testName = p.value[2]
          content += `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${p.color};margin-right:6px;"></span>${p.seriesName}: ${formatter(value)}`
          if (testName) content += `<br/><span style="font-size: 10px; color: ${isDark ? '#9ca3af' : '#6b7280'};">${testName}</span>`
          content += '<br/>'
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

    // Colors: A = blue palette, B = amber palette
    const colorA = '#3b82f6'
    const colorB = '#f59e0b'

    const cpuPercentOption = {
      ...baseConfig,
      tooltip: createTooltip((v) => `${v.toFixed(1)}%`),
      yAxis: createYAxis((value: number) => `${value.toFixed(0)}%`),
      series: [
        { name: 'Run A', ...lineA, data: pointsA.map((d) => [d.testIndex, d.cpuPercent, d.testName]), itemStyle: { color: colorA }, areaStyle: { opacity: 0.08, color: colorA } },
        { name: 'Run B', ...lineB, data: pointsB.map((d) => [d.testIndex, d.cpuPercent, d.testName]), itemStyle: { color: colorB }, areaStyle: { opacity: 0.08, color: colorB } },
      ],
    }

    const memoryMBOption = {
      ...baseConfig,
      tooltip: createTooltip((v) => `${v.toFixed(1)} MB`),
      yAxis: createYAxis((value: number) => `${value.toFixed(0)} MB`),
      series: [
        { name: 'Run A', ...lineA, data: pointsA.map((d) => [d.testIndex, d.memoryMB, d.testName]), itemStyle: { color: colorA }, areaStyle: { opacity: 0.08, color: colorA } },
        { name: 'Run B', ...lineB, data: pointsB.map((d) => [d.testIndex, d.memoryMB, d.testName]), itemStyle: { color: colorB }, areaStyle: { opacity: 0.08, color: colorB } },
      ],
    }

    const cpuTimeOption = {
      ...baseConfig,
      tooltip: createTooltip(formatMicroseconds),
      yAxis: createYAxis((value: number) => formatMicroseconds(value)),
      series: [
        { name: 'Run A', ...lineA, data: pointsA.map((d) => [d.testIndex, d.cpuUsec, d.testName]), itemStyle: { color: colorA }, areaStyle: { opacity: 0.08, color: colorA } },
        { name: 'Run B', ...lineB, data: pointsB.map((d) => [d.testIndex, d.cpuUsec, d.testName]), itemStyle: { color: colorB }, areaStyle: { opacity: 0.08, color: colorB } },
      ],
    }

    const memoryDeltaOption = {
      ...baseConfig,
      tooltip: createTooltip((v) => formatBytes(Math.abs(v)) + (v < 0 ? ' freed' : '')),
      yAxis: createYAxis((value: number) => formatBytes(Math.abs(value))),
      series: [
        { name: 'Run A', ...lineA, data: pointsA.map((d) => [d.testIndex, d.memoryDelta, d.testName]), itemStyle: { color: colorA }, areaStyle: { opacity: 0.08, color: colorA } },
        { name: 'Run B', ...lineB, data: pointsB.map((d) => [d.testIndex, d.memoryDelta, d.testName]), itemStyle: { color: colorB }, areaStyle: { opacity: 0.08, color: colorB } },
      ],
    }

    const diskBytesOption = {
      ...baseConfig,
      tooltip: createTooltip(formatBytes),
      yAxis: createYAxis((value: number) => formatBytes(value)),
      series: [
        { name: 'A Read', ...lineA, data: pointsA.map((d) => [d.testIndex, d.diskRead, d.testName]), itemStyle: { color: '#3b82f6' }, areaStyle: { opacity: 0.05, color: '#3b82f6' } },
        { name: 'A Write', ...lineA, data: pointsA.map((d) => [d.testIndex, d.diskWrite, d.testName]), itemStyle: { color: '#60a5fa' }, areaStyle: { opacity: 0.05, color: '#60a5fa' }, lineStyle: { width: 2, type: 'dashed' as const } },
        { name: 'B Read', ...lineB, data: pointsB.map((d) => [d.testIndex, d.diskRead, d.testName]), itemStyle: { color: '#f59e0b' }, areaStyle: { opacity: 0.05, color: '#f59e0b' } },
        { name: 'B Write', ...lineB, data: pointsB.map((d) => [d.testIndex, d.diskWrite, d.testName]), itemStyle: { color: '#fbbf24' }, areaStyle: { opacity: 0.05, color: '#fbbf24' }, lineStyle: { width: 2, type: 'dashed' as const } },
      ],
    }

    const diskOpsOption = {
      ...baseConfig,
      tooltip: createTooltip((v) => formatOps(v) + ' ops'),
      yAxis: createYAxis((value: number) => formatOps(value)),
      series: [
        { name: 'A Read Ops', ...lineA, data: pointsA.map((d) => [d.testIndex, d.diskReadOps, d.testName]), itemStyle: { color: '#3b82f6' }, areaStyle: { opacity: 0.05, color: '#3b82f6' } },
        { name: 'A Write Ops', ...lineA, data: pointsA.map((d) => [d.testIndex, d.diskWriteOps, d.testName]), itemStyle: { color: '#60a5fa' }, areaStyle: { opacity: 0.05, color: '#60a5fa' }, lineStyle: { width: 2, type: 'dashed' as const } },
        { name: 'B Read Ops', ...lineB, data: pointsB.map((d) => [d.testIndex, d.diskReadOps, d.testName]), itemStyle: { color: '#f59e0b' }, areaStyle: { opacity: 0.05, color: '#f59e0b' } },
        { name: 'B Write Ops', ...lineB, data: pointsB.map((d) => [d.testIndex, d.diskWriteOps, d.testName]), itemStyle: { color: '#fbbf24' }, areaStyle: { opacity: 0.05, color: '#fbbf24' }, lineStyle: { width: 2, type: 'dashed' as const } },
      ],
    }

    return { cpuPercentOption, memoryMBOption, cpuTimeOption, memoryDeltaOption, diskBytesOption, diskOpsOption }
  }, [pointsA, pointsB, isDark, zoomRange])

  if (!hasData) return null

  return (
    <div className="overflow-hidden rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
      <div className="mb-4 flex items-center gap-2">
        <Cpu className="size-4 text-gray-400 dark:text-gray-500" />
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Resource Usage Comparison</h3>
        <div className="ml-auto flex items-center gap-3 text-xs/5">
          <span className="flex items-center gap-1">
            <span className="inline-block size-2.5 rounded-full bg-blue-500" />
            <span className="text-gray-500 dark:text-gray-400">Run A</span>
          </span>
          <span className="flex items-center gap-1">
            <span className="inline-block size-2.5 rounded-full bg-amber-500" />
            <span className="text-gray-500 dark:text-gray-400">Run B</span>
          </span>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <ChartSection title="CPU Usage %" option={chartOptions.cpuPercentOption} onZoom={handleZoom} />
        <ChartSection title="Memory Usage (MB)" option={chartOptions.memoryMBOption} onZoom={handleZoom} />
        <ChartSection title="CPU Time" option={chartOptions.cpuTimeOption} onZoom={handleZoom} />
        <ChartSection title="Memory Delta" option={chartOptions.memoryDeltaOption} onZoom={handleZoom} />
        <ChartSection title="Disk I/O (Bytes)" option={chartOptions.diskBytesOption} onZoom={handleZoom} />
        <ChartSection title="Disk IOPS" option={chartOptions.diskOpsOption} onZoom={handleZoom} />
      </div>

      <p className="mt-4 text-center text-xs/5 text-gray-500 dark:text-gray-400">
        Resource usage per test (ordered by execution) - drag slider to zoom
      </p>
    </div>
  )
}
