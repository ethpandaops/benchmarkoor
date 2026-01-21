import { useMemo, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import clsx from 'clsx'
import type { IndexEntry } from '@/api/types'
import { formatBytes } from '@/utils/format'

export type XAxisMode = 'time' | 'runCount'

interface ResourceChartsProps {
  runs: IndexEntry[]
  isDark?: boolean
  xAxisMode?: XAxisMode
  onXAxisModeChange?: (mode: XAxisMode) => void
  onRunClick?: (runId: string) => void
}

interface DataPoint {
  timestamp: number
  value: number
  runIndex: number
  runId: string
  image: string
}

const CLIENT_COLORS: Record<string, string> = {
  geth: '#3b82f6',
  reth: '#f97316',
  nethermind: '#a855f7',
  besu: '#22c55e',
  erigon: '#ef4444',
  'nimbus-el': '#eab308',
}

const DEFAULT_COLOR = '#6b7280'

function capitalizeFirst(str: string): string {
  if (!str) return str
  return str.charAt(0).toUpperCase() + str.slice(1)
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

type MetricKey = 'cpu_usec' | 'memory_delta_bytes' | 'disk_read_bytes' | 'disk_write_bytes' | 'disk_read_iops' | 'disk_write_iops'

interface MetricConfig {
  key: MetricKey
  label: string
  formatter: (value: number) => string
  color: string
}

const METRICS: MetricConfig[] = [
  { key: 'cpu_usec', label: 'CPU Time', formatter: formatMicroseconds, color: '#8b5cf6' },
  { key: 'memory_delta_bytes', label: 'Memory Delta', formatter: (v) => formatBytes(Math.abs(v)), color: '#22c55e' },
  { key: 'disk_read_bytes', label: 'Disk Read', formatter: formatBytes, color: '#3b82f6' },
  { key: 'disk_write_bytes', label: 'Disk Write', formatter: formatBytes, color: '#f97316' },
  { key: 'disk_read_iops', label: 'Disk Read IOPS', formatter: formatOps, color: '#06b6d4' },
  { key: 'disk_write_iops', label: 'Disk Write IOPS', formatter: formatOps, color: '#ec4899' },
]

interface SingleChartProps {
  metric: MetricConfig
  runs: IndexEntry[]
  isDark: boolean
  xAxisMode: XAxisMode
  onRunClick?: (runId: string) => void
}

function SingleChart({ metric, runs, isDark, xAxisMode, onRunClick }: SingleChartProps) {
  const { clientGroups: chartData, maxRunIndex } = useMemo(() => {
    const clientGroups = new Map<string, DataPoint[]>()
    let maxRunIndex = 1

    for (const run of runs) {
      const resourceTotals = run.tests.resource_totals
      if (!resourceTotals) continue

      const value = resourceTotals[metric.key]
      if (value === undefined || value === null) continue

      const client = run.instance.client
      if (!clientGroups.has(client)) {
        clientGroups.set(client, [])
      }
      clientGroups.get(client)!.push({
        timestamp: run.timestamp * 1000,
        value,
        runIndex: 0,
        runId: run.run_id,
        image: run.instance.image,
      })
    }

    for (const [, data] of clientGroups) {
      data.sort((a, b) => a.timestamp - b.timestamp)
      const total = data.length
      if (total > maxRunIndex) {
        maxRunIndex = total
      }
      data.forEach((d, i) => {
        d.runIndex = total - i
      })
    }

    return { clientGroups, maxRunIndex }
  }, [runs, metric.key])

  const series = useMemo(() => {
    return Array.from(chartData.entries()).map(([client, data]) => ({
      name: capitalizeFirst(client),
      type: 'line' as const,
      data: data.map((d) =>
        xAxisMode === 'time'
          ? [d.timestamp, d.value, d.runIndex, d.runId, d.image]
          : [d.runIndex, d.value, d.timestamp, d.runId, d.image],
      ),
      showSymbol: true,
      symbolSize: 6,
      lineStyle: { width: 2 },
      itemStyle: { color: CLIENT_COLORS[client] ?? DEFAULT_COLOR },
      emphasis: {
        itemStyle: { borderWidth: 2, borderColor: '#fff' },
      },
      cursor: 'pointer',
    }))
  }, [chartData, xAxisMode])

  const option = useMemo(() => {
    const textColor = isDark ? '#e5e7eb' : '#374151'
    const axisLineColor = isDark ? '#4b5563' : '#d1d5db'
    const splitLineColor = isDark ? '#374151' : '#e5e7eb'

    const xAxisConfig =
      xAxisMode === 'time'
        ? {
            type: 'time' as const,
            axisLabel: {
              color: textColor,
              fontSize: 10,
              formatter: (value: number) => {
                const date = new Date(value)
                return `${date.getMonth() + 1}/${date.getDate()}`
              },
            },
          }
        : {
            type: 'value' as const,
            inverse: true,
            min: 1,
            max: maxRunIndex,
            minInterval: 1,
            axisLabel: {
              color: textColor,
              fontSize: 10,
              formatter: (value: number) => `#${value}`,
            },
          }

    return {
      backgroundColor: 'transparent',
      tooltip: {
        trigger: 'axis',
        appendToBody: true,
        backgroundColor: isDark ? '#1f2937' : '#ffffff',
        borderColor: isDark ? '#374151' : '#e5e7eb',
        textStyle: { color: textColor },
        formatter: (params: Array<{ seriesName: string; color: string; value: [number, number, number, string, string] }>) => {
          if (!params || params.length === 0) return ''
          const first = params[0]
          const [xVal, , extraVal] = first.value
          const date = xAxisMode === 'time' ? new Date(xVal).toLocaleString() : new Date(extraVal).toLocaleString()
          const runNum = xAxisMode === 'time' ? extraVal : xVal

          let html = `<div style="margin-bottom: 4px;"><strong>Run #${runNum}</strong></div>`
          html += `<div style="margin-bottom: 8px; color: ${isDark ? '#9ca3af' : '#6b7280'}; font-size: 11px;">${date}</div>`

          for (const p of params) {
            const [, value, , , image] = p.value
            html += `<div style="display: flex; align-items: center; gap: 6px; margin-bottom: 4px;">`
            html += `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${p.color};"></span>`
            html += `<strong>${p.seriesName}:</strong> ${metric.formatter(value)}`
            html += `</div>`
            html += `<div style="color: ${isDark ? '#9ca3af' : '#6b7280'}; font-size: 10px; margin-left: 16px; margin-bottom: 6px;">Image: ${image}</div>`
          }

          html += `<div style="color: ${isDark ? '#60a5fa' : '#3b82f6'}; font-size: 11px; margin-top: 4px;">Click to view details</div>`
          return html
        },
      },
      legend: {
        type: 'scroll',
        bottom: 0,
        textStyle: { color: textColor, fontSize: 10 },
        itemWidth: 12,
        itemHeight: 8,
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: '18%',
        top: '10%',
        containLabel: true,
      },
      xAxis: {
        ...xAxisConfig,
        axisLine: { lineStyle: { color: axisLineColor } },
        splitLine: { show: false },
      },
      yAxis: {
        type: 'value',
        axisLabel: {
          color: textColor,
          fontSize: 10,
          formatter: (value: number) => metric.formatter(value),
        },
        axisLine: { lineStyle: { color: axisLineColor } },
        splitLine: { lineStyle: { color: splitLineColor } },
      },
      series,
    }
  }, [series, isDark, xAxisMode, metric, maxRunIndex])

  const handleChartClick = (params: { value?: [number, number, number, string, string] }) => {
    if (params.value && onRunClick) {
      const runId = params.value[3]
      if (runId) {
        onRunClick(runId)
      }
    }
  }

  if (series.length === 0) {
    return null
  }

  return (
    <div className="rounded-xs bg-gray-50 p-3 dark:bg-gray-700/50">
      <h4 className="mb-2 text-xs font-medium text-gray-700 dark:text-gray-300">{metric.label}</h4>
      <ReactECharts
        option={option}
        style={{ height: '200px', width: '100%' }}
        opts={{ renderer: 'svg' }}
        onEvents={{ click: handleChartClick }}
      />
    </div>
  )
}

export function ResourceCharts({
  runs,
  isDark = false,
  xAxisMode: controlledMode,
  onXAxisModeChange,
  onRunClick,
}: ResourceChartsProps) {
  const [internalMode, setInternalMode] = useState<XAxisMode>('runCount')
  const xAxisMode = controlledMode ?? internalMode

  const setXAxisMode = (mode: XAxisMode) => {
    if (onXAxisModeChange) {
      onXAxisModeChange(mode)
    } else {
      setInternalMode(mode)
    }
  }

  const hasResourceData = useMemo(() => {
    return runs.some((run) => run.tests.resource_totals)
  }, [runs])

  if (!hasResourceData) {
    return (
      <div className="flex h-32 items-center justify-center text-sm/6 text-gray-500 dark:text-gray-400">
        No resource usage data available
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex justify-end">
        <div className="inline-flex rounded-sm border border-gray-300 dark:border-gray-600">
          <button
            onClick={() => setXAxisMode('runCount')}
            className={clsx(
              'px-3 py-1 text-xs/5 font-medium transition-colors',
              xAxisMode === 'runCount'
                ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900'
                : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700',
            )}
          >
            Run #
          </button>
          <button
            onClick={() => setXAxisMode('time')}
            className={clsx(
              'border-l border-gray-300 px-3 py-1 text-xs/5 font-medium transition-colors dark:border-gray-600',
              xAxisMode === 'time'
                ? 'bg-gray-900 text-white dark:bg-gray-100 dark:text-gray-900'
                : 'bg-white text-gray-700 hover:bg-gray-50 dark:bg-gray-800 dark:text-gray-300 dark:hover:bg-gray-700',
            )}
          >
            Time
          </button>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {METRICS.map((metric) => (
          <SingleChart
            key={metric.key}
            metric={metric}
            runs={runs}
            isDark={isDark}
            xAxisMode={xAxisMode}
            onRunClick={onRunClick}
          />
        ))}
      </div>

      {xAxisMode === 'runCount' && (
        <div className="flex justify-end text-xs/5 text-gray-500 dark:text-gray-400">
          <span>← Older runs | More recent →</span>
        </div>
      )}
    </div>
  )
}
