import { useMemo, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import clsx from 'clsx'
import { type IndexEntry, type IndexStepType, getIndexAggregatedStats, ALL_INDEX_STEP_TYPES } from '@/api/types'

export type XAxisMode = 'time' | 'runCount'

interface MGasChartProps {
  runs: IndexEntry[]
  isDark?: boolean
  xAxisMode?: XAxisMode
  onXAxisModeChange?: (mode: XAxisMode) => void
  onRunClick?: (runId: string) => void
  stepFilter?: IndexStepType[]
  hideControls?: boolean
}

interface DataPoint {
  timestamp: number
  mgasPerSec: number
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

function calculateMGasPerSec(gasUsed: number, gasUsedDuration: number): number | undefined {
  if (gasUsedDuration <= 0 || gasUsed <= 0) return undefined
  return (gasUsed * 1000) / gasUsedDuration
}

export function MGasChart({
  runs,
  isDark = false,
  xAxisMode: controlledMode,
  onXAxisModeChange,
  onRunClick,
  stepFilter = ALL_INDEX_STEP_TYPES,
  hideControls = false,
}: MGasChartProps) {
  const [internalMode, setInternalMode] = useState<XAxisMode>('runCount')
  const xAxisMode = controlledMode ?? internalMode

  const setXAxisMode = (mode: XAxisMode) => {
    if (onXAxisModeChange) {
      onXAxisModeChange(mode)
    } else {
      setInternalMode(mode)
    }
  }

  const { clientGroups: chartData, maxRunIndex } = useMemo(() => {
    const clientGroups = new Map<string, DataPoint[]>()
    let maxRunIndex = 1

    for (const run of runs) {
      const stats = getIndexAggregatedStats(run, stepFilter)
      const mgasPerSec = calculateMGasPerSec(stats.gasUsed, stats.gasUsedDuration)
      if (mgasPerSec === undefined) continue

      const client = run.instance.client
      if (!clientGroups.has(client)) {
        clientGroups.set(client, [])
      }
      clientGroups.get(client)!.push({
        timestamp: run.timestamp * 1000,
        mgasPerSec,
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

    return { clientGroups, maxRunIndex }
  }, [runs, stepFilter])

  const series = useMemo(() => {
    return Array.from(chartData.entries()).map(([client, data]) => ({
      name: capitalizeFirst(client),
      type: 'line' as const,
      data: data.map((d) =>
        xAxisMode === 'time'
          ? [d.timestamp, d.mgasPerSec, d.runIndex, d.runId, d.image]
          : [d.runIndex, d.mgasPerSec, d.timestamp, d.runId, d.image],
      ),
      showSymbol: true,
      symbolSize: 8,
      lineStyle: {
        width: 2,
      },
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
            const [, mgasPerSec, , , image] = p.value
            html += `<div style="display: flex; align-items: center; gap: 6px; margin-bottom: 4px;">`
            html += `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${p.color};"></span>`
            html += `<strong>${p.seriesName}:</strong> ${mgasPerSec.toFixed(2)} MGas/s`
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
        textStyle: {
          color: textColor,
        },
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: '15%',
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
        name: 'MGas/s',
        nameTextStyle: {
          color: textColor,
        },
        axisLabel: {
          color: textColor,
          formatter: (value: number) => value.toFixed(1),
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
      series,
    }
  }, [series, isDark, xAxisMode, maxRunIndex])

  const handleChartClick = (params: { value?: [number, number, number, string, string] }) => {
    if (params.value && onRunClick) {
      const runId = params.value[3]
      if (runId) {
        onRunClick(runId)
      }
    }
  }

  // Check if any runs have MGas/s data
  const hasData = Array.from(chartData.values()).some((data) => data.length > 0)

  if (!hasData) {
    return (
      <div className="flex h-64 items-center justify-center text-sm/6 text-gray-500 dark:text-gray-400">
        No MGas/s data available for chart
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-2">
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
        style={{ height: '300px', width: '100%' }}
        opts={{ renderer: 'svg' }}
        onEvents={{ click: handleChartClick }}
      />
      {!hideControls && xAxisMode === 'runCount' && (
        <div className="flex justify-end text-xs/5 text-gray-500 dark:text-gray-400">
          <span>← Older runs | More recent →</span>
        </div>
      )}
    </div>
  )
}
