import { useCallback, useMemo, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import clsx from 'clsx'
import { type IndexEntry, type IndexStepType, getIndexAggregatedStats, ALL_INDEX_STEP_TYPES } from '@/api/types'

export type XAxisMode = 'time' | 'runCount'

interface DurationChartProps {
  runs: IndexEntry[]
  isDark?: boolean
  xAxisMode?: XAxisMode
  onXAxisModeChange?: (mode: XAxisMode) => void
  onRunClick?: (runId: string) => void
  stepFilter?: IndexStepType[]
  hideControls?: boolean
  zoomRange?: { start: number; end: number }
  onZoomChange?: (range: { start: number; end: number }) => void
}

interface DataPoint {
  timestamp: number
  duration: number
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
  nimbus: '#eab308',
}

const DEFAULT_COLOR = '#6b7280'

function capitalizeFirst(str: string): string {
  if (!str) return str
  return str.charAt(0).toUpperCase() + str.slice(1)
}

function formatDuration(seconds: number): string {
  if (seconds < 60) {
    return `${seconds.toFixed(1)}s`
  }
  const minutes = Math.floor(seconds / 60)
  const remainingSeconds = seconds % 60
  return `${minutes}m ${remainingSeconds.toFixed(0)}s`
}

export function DurationChart({
  runs,
  isDark = false,
  xAxisMode: controlledMode,
  onXAxisModeChange,
  onRunClick,
  stepFilter = ALL_INDEX_STEP_TYPES,
  hideControls = false,
  zoomRange: controlledZoom,
  onZoomChange,
}: DurationChartProps) {
  const [internalMode, setInternalMode] = useState<XAxisMode>('runCount')
  const xAxisMode = controlledMode ?? internalMode

  const setXAxisMode = (mode: XAxisMode) => {
    if (onXAxisModeChange) {
      onXAxisModeChange(mode)
    } else {
      setInternalMode(mode)
    }
  }

  const [internalZoom, setInternalZoom] = useState({ start: 0, end: 100 })
  const activeZoom = controlledZoom ?? internalZoom

  const { clientGroups: chartData, maxRunIndex, isLargeDataset } = useMemo(() => {
    const clientGroups = new Map<string, DataPoint[]>()
    let maxRunIndex = 1

    for (const run of runs) {
      const client = run.instance.client
      const stats = getIndexAggregatedStats(run, stepFilter)
      if (!clientGroups.has(client)) {
        clientGroups.set(client, [])
      }
      clientGroups.get(client)!.push({
        timestamp: run.timestamp * 1000,
        duration: stats.duration / 1e9,
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
        // Run #1 = most recent, Run #N = oldest
        d.runIndex = total - i
      })
    }

    const maxSeriesLength = Math.max(...Array.from(clientGroups.values()).map(d => d.length), 0)
    const isLargeDataset = maxSeriesLength > 30

    return { clientGroups, maxRunIndex, isLargeDataset }
  }, [runs, stepFilter])

  const series = useMemo(() => {
    return Array.from(chartData.entries()).map(([client, data]) => ({
      name: capitalizeFirst(client),
      type: 'line' as const,
      data: data.map((d) =>
        xAxisMode === 'time'
          ? [d.timestamp, d.duration, d.runIndex, d.runId, d.image]
          : [d.runIndex, d.duration, d.timestamp, d.runId, d.image],
      ),
      showSymbol: !isLargeDataset,
      symbolSize: isLargeDataset ? 4 : 8,
      lineStyle: {
        width: isLargeDataset ? 1.5 : 2,
      },
      sampling: isLargeDataset ? ('lttb' as const) : undefined,
      itemStyle: {
        color: CLIENT_COLORS[client] ?? DEFAULT_COLOR,
      },
      emphasis: {
        itemStyle: {
          borderWidth: 2,
          borderColor: '#fff',
        },
      },
      cursor: 'pointer',
    }))
  }, [chartData, xAxisMode, isLargeDataset])

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
              formatter: (value: number) => {
                const date = new Date(value)
                return `${date.getMonth() + 1}/${date.getDate()}`
              },
            },
          }
        : {
            type: 'value' as const,
            name: 'Run #',
            nameTextStyle: {
              color: textColor,
            },
            inverse: true,
            min: 1,
            max: maxRunIndex,
            minInterval: 1,
            axisLabel: {
              color: textColor,
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
        textStyle: {
          color: textColor,
        },
        formatter: (params: Array<{ seriesName: string; color: string; value: [number, number, number, string, string] }>) => {
          if (!params || params.length === 0) return ''
          const first = params[0]
          const [xVal, , extraVal] = first.value
          const date = xAxisMode === 'time' ? new Date(xVal).toLocaleString() : new Date(extraVal).toLocaleString()
          const runNum = xAxisMode === 'time' ? extraVal : xVal

          let html = `<div style="margin-bottom: 4px;"><strong>Run #${runNum}</strong></div>`
          html += `<div style="margin-bottom: 8px; color: ${isDark ? '#9ca3af' : '#6b7280'}; font-size: 11px;">${date}</div>`

          for (const p of params) {
            const [, duration, , , image] = p.value
            const durationStr = formatDuration(duration)
            html += `<div style="display: flex; align-items: center; gap: 6px; margin-bottom: 4px;">`
            html += `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${p.color};"></span>`
            html += `<strong>${p.seriesName}:</strong> ${durationStr}`
            html += `</div>`
            html += `<div style="color: ${isDark ? '#9ca3af' : '#6b7280'}; font-size: 10px; margin-left: 16px; margin-bottom: 6px;">Image: ${image}</div>`
          }

          html += `<div style="color: ${isDark ? '#60a5fa' : '#3b82f6'}; font-size: 11px; margin-top: 4px;">Click to view details</div>`
          return html
        },
      },
      legend: {
        type: 'scroll',
        bottom: isLargeDataset ? 25 : 0,
        textStyle: {
          color: textColor,
        },
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: isLargeDataset ? 50 : '15%',
        top: '10%',
        containLabel: true,
      },
      xAxis: {
        ...xAxisConfig,
        axisLine: {
          lineStyle: {
            color: axisLineColor,
          },
        },
        splitLine: {
          show: false,
        },
      },
      yAxis: {
        type: 'value',
        axisLabel: {
          color: textColor,
          formatter: (value: number) => formatDuration(value),
        },
        axisLine: {
          lineStyle: {
            color: axisLineColor,
          },
        },
        splitLine: {
          lineStyle: {
            color: splitLineColor,
          },
        },
      },
      ...(isLargeDataset
        ? {
            dataZoom: [
              {
                type: 'slider' as const,
                xAxisIndex: 0,
                start: activeZoom.start,
                end: activeZoom.end,
                height: 20,
                bottom: 5,
                borderColor: axisLineColor,
                fillerColor: isDark ? 'rgba(59, 130, 246, 0.3)' : 'rgba(59, 130, 246, 0.1)',
                backgroundColor: isDark ? '#374151' : '#f3f4f6',
                dataBackground: {
                  lineStyle: { color: isDark ? '#6b7280' : '#9ca3af' },
                  areaStyle: { color: isDark ? '#4b5563' : '#e5e7eb' },
                },
                selectedDataBackground: {
                  lineStyle: { color: '#3b82f6' },
                  areaStyle: { color: isDark ? 'rgba(59, 130, 246, 0.3)' : 'rgba(59, 130, 246, 0.2)' },
                },
                handleStyle: { color: '#3b82f6', borderColor: '#3b82f6' },
                moveHandleStyle: { color: isDark ? '#9ca3af' : '#6b7280' },
                emphasis: {
                  handleStyle: { color: '#60a5fa', borderColor: '#60a5fa' },
                },
                textStyle: { color: textColor },
              },
              {
                type: 'inside' as const,
                xAxisIndex: 0,
                start: activeZoom.start,
                end: activeZoom.end,
                zoomOnMouseWheel: true,
                moveOnMouseMove: true,
                moveOnMouseWheel: false,
              },
            ],
          }
        : {}),
      animation: !isLargeDataset,
      series,
    }
  }, [series, isDark, xAxisMode, maxRunIndex, isLargeDataset, activeZoom])

  const handleDataZoom = useCallback(
    (params: { start?: number; end?: number; batch?: Array<{ start: number; end: number }> }) => {
      let newRange: { start: number; end: number } | undefined
      if (params.batch && params.batch.length > 0) {
        newRange = { start: params.batch[0].start, end: params.batch[0].end }
      } else if (params.start !== undefined && params.end !== undefined) {
        newRange = { start: params.start, end: params.end }
      }
      if (newRange) {
        if (onZoomChange) onZoomChange(newRange)
        else setInternalZoom(newRange)
      }
    },
    [onZoomChange],
  )

  const handleChartClick = (params: { value?: [number, number, number, string, string] }) => {
    if (params.value && onRunClick) {
      const runId = params.value[3]
      if (runId) {
        onRunClick(runId)
      }
    }
  }

  if (runs.length === 0) {
    return (
      <div className="flex h-64 items-center justify-center text-sm/6 text-gray-500 dark:text-gray-400">
        No run data available for chart
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      <h4 className="text-xs font-medium text-gray-700 dark:text-gray-300">Duration</h4>
      {!hideControls && (
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
      )}
      <ReactECharts
        option={option}
        style={{ height: '250px', width: '100%' }}
        opts={{ renderer: 'svg' }}
        onEvents={{ click: handleChartClick, ...(isLargeDataset && { datazoom: handleDataZoom }) }}
      />
      {!hideControls && xAxisMode === 'runCount' && (
        <div className="flex justify-end text-xs/5 text-gray-500 dark:text-gray-400">
          <span>← Older runs | More recent →</span>
        </div>
      )}
    </div>
  )
}
