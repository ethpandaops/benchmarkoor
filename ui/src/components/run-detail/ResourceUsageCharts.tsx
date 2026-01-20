import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import type { TestEntry } from '@/api/types'
import { formatBytes } from '@/utils/format'

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

interface ResourceUsageChartsProps {
  tests: Record<string, TestEntry>
  onTestClick?: (testName: string) => void
}

interface ResourceDataPoint {
  testIndex: number
  testName: string
  cpuUsec: number
  memoryDelta: number
  diskRead: number
  diskWrite: number
  diskReadOps: number
  diskWriteOps: number
}

interface SummaryStats {
  totalCpu: number
  peakMemory: number
  totalDiskRead: number
  totalDiskWrite: number
  totalReadOps: number
  totalWriteOps: number
}

function formatMicroseconds(usec: number): string {
  if (usec < 1000) {
    return `${usec.toFixed(0)} µs`
  }
  if (usec < 1_000_000) {
    return `${(usec / 1000).toFixed(1)} ms`
  }
  return `${(usec / 1_000_000).toFixed(2)} s`
}

function formatOps(ops: number): string {
  if (ops < 1000) {
    return `${ops.toFixed(0)}`
  }
  if (ops < 1_000_000) {
    return `${(ops / 1000).toFixed(1)}K`
  }
  return `${(ops / 1_000_000).toFixed(1)}M`
}

interface StatCardProps {
  label: string
  value: string
}

function StatCard({ label, value }: StatCardProps) {
  return (
    <div className="rounded-xs bg-gray-50 px-3 py-2 dark:bg-gray-700/50">
      <div className="text-xs text-gray-500 dark:text-gray-400">{label}</div>
      <div className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">{value}</div>
    </div>
  )
}

interface ChartSectionProps {
  title: string
  option: object
  onZoom: (start: number, end: number) => void
  onPointClick?: (testName: string) => void
  highlightedTestRef: React.MutableRefObject<string | null>
}

function ChartSection({ title, option, onZoom, onPointClick, highlightedTestRef }: ChartSectionProps) {
  const onEvents = useMemo(
    () => ({
      datazoom: (params: { start?: number; end?: number; batch?: Array<{ start: number; end: number }> }) => {
        // Handle both single and batch zoom events
        if (params.batch && params.batch.length > 0) {
          onZoom(params.batch[0].start, params.batch[0].end)
        } else if (params.start !== undefined && params.end !== undefined) {
          onZoom(params.start, params.end)
        }
      },
    }),
    [onZoom],
  )

  const handleContainerClick = useCallback(() => {
    if (onPointClick && highlightedTestRef.current) {
      onPointClick(highlightedTestRef.current)
    }
  }, [onPointClick, highlightedTestRef])

  return (
    <div className="rounded-xs bg-gray-50 p-3 dark:bg-gray-700/50">
      <h4 className="mb-2 text-xs font-medium text-gray-700 dark:text-gray-300">{title}</h4>
      <div onClick={handleContainerClick} style={{ cursor: onPointClick ? 'pointer' : 'default' }}>
        <ReactECharts
          option={option}
          style={{ height: '200px', width: '100%' }}
          opts={{ renderer: 'svg' }}
          onEvents={onEvents}
        />
      </div>
    </div>
  )
}

export function ResourceUsageCharts({ tests, onTestClick }: ResourceUsageChartsProps) {
  const isDark = useDarkMode()
  const [zoomRange, setZoomRange] = useState({ start: 0, end: 100 })
  const highlightedTestRef = useRef<string | null>(null)

  const handleZoom = useCallback((start: number, end: number) => {
    setZoomRange({ start, end })
  }, [])

  const { dataPoints, hasResourceData, summaryStats } = useMemo(() => {
    const points: ResourceDataPoint[] = []
    let hasData = false
    const stats: SummaryStats = {
      totalCpu: 0,
      peakMemory: 0,
      totalDiskRead: 0,
      totalDiskWrite: 0,
      totalReadOps: 0,
      totalWriteOps: 0,
    }

    // Sort tests by their directory order (numeric prefix)
    const sortedTests = Object.entries(tests).sort(([, a], [, b]) => {
      const aNum = parseInt(a.dir, 10) || 0
      const bNum = parseInt(b.dir, 10) || 0
      return aNum - bNum
    })

    sortedTests.forEach(([testName, test], index) => {
      const agg = test.aggregated
      if (
        agg.cpu_usec_total !== undefined ||
        agg.memory_delta_total !== undefined ||
        agg.disk_read_total !== undefined ||
        agg.disk_write_total !== undefined
      ) {
        hasData = true
        const cpuUsec = agg.cpu_usec_total ?? 0
        const memoryDelta = agg.memory_delta_total ?? 0
        const diskRead = agg.disk_read_total ?? 0
        const diskWrite = agg.disk_write_total ?? 0
        const diskReadOps = agg.disk_read_iops_total ?? 0
        const diskWriteOps = agg.disk_write_iops_total ?? 0

        points.push({
          testIndex: index + 1,
          testName,
          cpuUsec,
          memoryDelta,
          diskRead,
          diskWrite,
          diskReadOps,
          diskWriteOps,
        })

        // Update summary stats
        stats.totalCpu += cpuUsec
        if (memoryDelta > stats.peakMemory) {
          stats.peakMemory = memoryDelta
        }
        stats.totalDiskRead += diskRead
        stats.totalDiskWrite += diskWrite
        stats.totalReadOps += diskReadOps
        stats.totalWriteOps += diskWriteOps
      }
    })

    return { dataPoints: points, hasResourceData: hasData, summaryStats: stats }
  }, [tests])

  const chartOptions = useMemo(() => {
    const textColor = isDark ? '#ffffff' : '#374151'
    const axisLineColor = isDark ? '#4b5563' : '#d1d5db'
    const splitLineColor = isDark ? '#374151' : '#e5e7eb'
    const isLargeDataset = dataPoints.length > 100

    const baseConfig = {
      backgroundColor: 'transparent',
      animation: !isLargeDataset, // Disable animation for large datasets
      textStyle: {
        color: textColor, // Global default text color
      },
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
        max: dataPoints.length,
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
          dataBackground: {
            lineStyle: { color: isDark ? '#6b7280' : '#9ca3af' },
            areaStyle: { color: isDark ? '#4b5563' : '#e5e7eb' },
          },
          selectedDataBackground: {
            lineStyle: { color: '#8b5cf6' },
            areaStyle: { color: isDark ? 'rgba(139, 92, 246, 0.3)' : 'rgba(139, 92, 246, 0.2)' },
          },
          handleStyle: {
            color: '#8b5cf6',
            borderColor: '#8b5cf6',
          },
          moveHandleStyle: {
            color: isDark ? '#9ca3af' : '#6b7280',
          },
          emphasis: {
            handleStyle: {
              color: '#a78bfa',
              borderColor: '#a78bfa',
            },
            moveHandleStyle: {
              color: isDark ? '#d1d5db' : '#374151',
            },
          },
          textStyle: {
            color: textColor,
          },
          labelFormatter: (value: number) => `#${Math.round(value)}`,
        },
      ],
    }

    const createTooltip = (
      formatter: (value: number) => string,
      total: number,
    ) => ({
      trigger: 'axis' as const,
      backgroundColor: isDark ? '#1f2937' : '#ffffff',
      borderColor: isDark ? '#374151' : '#e5e7eb',
      textStyle: { color: textColor },
      extraCssText: 'max-width: 300px; white-space: normal;',
      formatter: (
        params: Array<{ seriesName: string; color: string; value: [number, number, string] }>,
      ) => {
        if (!params.length) return ''
        const testName = params[0].value[2]
        const testIndex = params[0].value[0]
        // Track the highlighted test for click handling
        highlightedTestRef.current = testName
        let content = `<strong>Test #${testIndex}</strong><br/><span style="font-size: 11px; color: ${isDark ? '#9ca3af' : '#6b7280'}; word-break: break-all; display: block;">${testName}</span><br/>`
        params.forEach((p) => {
          const value = p.value[1]
          const formatted = formatter(value)
          const percentage = total > 0 ? ((value / total) * 100).toFixed(1) : '0.0'
          content += `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${p.color};margin-right:6px;"></span>${p.seriesName}: ${formatted} (${percentage}%)<br/>`
        })
        return content
      },
    })

    const createMultiSeriesTooltip = (
      formatter: (value: number) => string,
      totals: number[],
    ) => ({
      trigger: 'axis' as const,
      backgroundColor: isDark ? '#1f2937' : '#ffffff',
      borderColor: isDark ? '#374151' : '#e5e7eb',
      textStyle: { color: textColor },
      extraCssText: 'max-width: 300px; white-space: normal;',
      formatter: (
        params: Array<{ seriesName: string; color: string; value: [number, number, string]; seriesIndex: number }>,
      ) => {
        if (!params.length) return ''
        const testName = params[0].value[2]
        const testIndex = params[0].value[0]
        // Track the highlighted test for click handling
        highlightedTestRef.current = testName
        let content = `<strong>Test #${testIndex}</strong><br/><span style="font-size: 11px; color: ${isDark ? '#9ca3af' : '#6b7280'}; word-break: break-all; display: block;">${testName}</span><br/>`
        params.forEach((p) => {
          const value = p.value[1]
          const formatted = formatter(value)
          const total = totals[p.seriesIndex] || 0
          const percentage = total > 0 ? ((value / total) * 100).toFixed(1) : '0.0'
          content += `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${p.color};margin-right:6px;"></span>${p.seriesName}: ${formatted} (${percentage}%)<br/>`
        })
        return content
      },
    })

    // Optimize for large datasets
    const lineSeriesConfig = {
      type: 'line' as const,
      smooth: !isLargeDataset, // Disable smoothing for large datasets
      showSymbol: !isLargeDataset, // Hide symbols for large datasets
      symbolSize: isLargeDataset ? 4 : 6,
      lineStyle: { width: isLargeDataset ? 1.5 : 2 },
      sampling: isLargeDataset ? ('lttb' as const) : undefined, // Downsample large datasets
      large: isLargeDataset,
      largeThreshold: 100,
      emphasis: {
        focus: 'series' as const,
        itemStyle: { borderWidth: 2 },
      },
    }

    // Common yAxis config
    const createYAxis = (formatter: (value: number) => string) => ({
      type: 'value' as const,
      axisLabel: {
        color: textColor,
        fontSize: 11,
        formatter,
      },
      axisLine: { show: true, lineStyle: { color: axisLineColor } },
      axisTick: { show: true, lineStyle: { color: axisLineColor } },
      splitLine: { lineStyle: { color: splitLineColor } },
    })

    // CPU Usage Chart
    const cpuOption = {
      ...baseConfig,
      tooltip: createTooltip(formatMicroseconds, summaryStats.totalCpu),
      yAxis: createYAxis((value: number) => formatMicroseconds(value)),
      series: [
        {
          name: 'CPU Usage',
          ...lineSeriesConfig,
          data: dataPoints.map((d) => [d.testIndex, d.cpuUsec, d.testName]),
          itemStyle: { color: '#8b5cf6' },
          areaStyle: { opacity: 0.1, color: '#8b5cf6' },
        },
      ],
    }

    // Memory Delta Chart
    const memoryOption = {
      ...baseConfig,
      tooltip: createTooltip(
        (v) => formatBytes(Math.abs(v)) + (v < 0 ? ' freed' : ''),
        summaryStats.peakMemory,
      ),
      yAxis: createYAxis((value: number) => formatBytes(Math.abs(value))),
      series: [
        {
          name: 'Memory Delta',
          ...lineSeriesConfig,
          data: dataPoints.map((d) => [d.testIndex, d.memoryDelta, d.testName]),
          itemStyle: { color: '#22c55e' },
          areaStyle: { opacity: 0.1, color: '#22c55e' },
        },
      ],
    }

    // Disk I/O (Bytes) Chart
    const diskBytesOption = {
      ...baseConfig,
      tooltip: createMultiSeriesTooltip(formatBytes, [
        summaryStats.totalDiskRead,
        summaryStats.totalDiskWrite,
      ]),
      legend: {
        bottom: 25,
        textStyle: { color: textColor, fontSize: 11 },
        itemWidth: 12,
        itemHeight: 8,
      },
      yAxis: createYAxis((value: number) => formatBytes(value)),
      series: [
        {
          name: 'Disk Read',
          ...lineSeriesConfig,
          data: dataPoints.map((d) => [d.testIndex, d.diskRead, d.testName]),
          itemStyle: { color: '#3b82f6' },
          areaStyle: { opacity: 0.1, color: '#3b82f6' },
        },
        {
          name: 'Disk Write',
          ...lineSeriesConfig,
          data: dataPoints.map((d) => [d.testIndex, d.diskWrite, d.testName]),
          itemStyle: { color: '#f97316' },
          areaStyle: { opacity: 0.1, color: '#f97316' },
        },
      ],
    }

    // Disk IOPS Chart
    const diskOpsOption = {
      ...baseConfig,
      tooltip: createMultiSeriesTooltip((v) => formatOps(v) + ' ops', [
        summaryStats.totalReadOps,
        summaryStats.totalWriteOps,
      ]),
      legend: {
        bottom: 25,
        textStyle: { color: textColor, fontSize: 11 },
        itemWidth: 12,
        itemHeight: 8,
      },
      yAxis: createYAxis((value: number) => formatOps(value)),
      series: [
        {
          name: 'Read Ops',
          ...lineSeriesConfig,
          data: dataPoints.map((d) => [d.testIndex, d.diskReadOps, d.testName]),
          itemStyle: { color: '#06b6d4' },
          areaStyle: { opacity: 0.1, color: '#06b6d4' },
        },
        {
          name: 'Write Ops',
          ...lineSeriesConfig,
          data: dataPoints.map((d) => [d.testIndex, d.diskWriteOps, d.testName]),
          itemStyle: { color: '#ec4899' },
          areaStyle: { opacity: 0.1, color: '#ec4899' },
        },
      ],
    }

    return { cpuOption, memoryOption, diskBytesOption, diskOpsOption }
  }, [dataPoints, isDark, summaryStats, zoomRange])

  if (!hasResourceData) {
    return null
  }

  return (
    <div className="overflow-hidden rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
      <h3 className="mb-4 text-sm/6 font-medium text-gray-900 dark:text-gray-100">Resource Usage</h3>

      {/* Summary Stats Row */}
      <div className="mb-4 grid grid-cols-3 gap-2 sm:grid-cols-6">
        <StatCard label="CPU Time" value={formatMicroseconds(summaryStats.totalCpu)} />
        <StatCard label="Memory Peak" value={formatBytes(summaryStats.peakMemory)} />
        <StatCard label="Disk Read" value={formatBytes(summaryStats.totalDiskRead)} />
        <StatCard label="Disk Write" value={formatBytes(summaryStats.totalDiskWrite)} />
        <StatCard label="Read Ops" value={formatOps(summaryStats.totalReadOps)} />
        <StatCard label="Write Ops" value={formatOps(summaryStats.totalWriteOps)} />
      </div>

      {/* Charts Grid */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <ChartSection title="CPU Usage" option={chartOptions.cpuOption} onZoom={handleZoom} onPointClick={onTestClick} highlightedTestRef={highlightedTestRef} />
        <ChartSection title="Memory Delta" option={chartOptions.memoryOption} onZoom={handleZoom} onPointClick={onTestClick} highlightedTestRef={highlightedTestRef} />
        <ChartSection title="Disk I/O (Bytes)" option={chartOptions.diskBytesOption} onZoom={handleZoom} onPointClick={onTestClick} highlightedTestRef={highlightedTestRef} />
        <ChartSection title="Disk IOPS" option={chartOptions.diskOpsOption} onZoom={handleZoom} onPointClick={onTestClick} highlightedTestRef={highlightedTestRef} />
      </div>

      <p className="mt-4 text-center text-xs/5 text-gray-500 dark:text-gray-400">
        Resource usage per test (ordered by execution) - drag slider to zoom
      </p>
    </div>
  )
}
