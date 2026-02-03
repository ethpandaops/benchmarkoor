import { useMemo, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import type { ProcessedTestData } from '../types'
import { ALL_CATEGORIES, CATEGORY_COLORS } from '../utils/colors'

type SortMode = 'order' | 'throughput'

interface ThroughputBarChartProps {
  data: ProcessedTestData[]
  isDark: boolean
  useLogScale: boolean
  onTestClick?: (testName: string) => void
}

export function ThroughputBarChart({ data, isDark, useLogScale, onTestClick }: ThroughputBarChartProps) {
  const [sortMode, setSortMode] = useState<SortMode>('throughput')

  const textColor = isDark ? '#e5e7eb' : '#374151'
  const subTextColor = isDark ? '#9ca3af' : '#6b7280'
  const gridColor = isDark ? '#374151' : '#e5e7eb'
  const tooltipBg = isDark ? '#1f2937' : '#ffffff'
  const tooltipBorder = isDark ? '#374151' : '#e5e7eb'

  const chartData = useMemo(() => {
    return [...data].sort((a, b) => {
      if (sortMode === 'order') {
        return a.testOrder - b.testOrder
      }
      return a.throughput - b.throughput
    })
  }, [data, sortMode])

  const option = useMemo(() => {
    const testLabels = chartData.map((d) =>
      d.testOrder === Infinity ? '-' : `#${d.testOrder}`
    )

    return {
      tooltip: {
        trigger: 'axis' as const,
        axisPointer: { type: 'shadow' as const },
        backgroundColor: tooltipBg,
        borderColor: tooltipBorder,
        textStyle: { color: textColor },
        extraCssText: 'max-width: 300px; white-space: normal;',
        formatter: (params: { name: string; value: number; dataIndex: number }[]) => {
          const param = params[0]
          const item = chartData[param.dataIndex]
          const testLabel = item.testOrder === Infinity ? '-' : `#${item.testOrder}`
          return `
            <strong>Test ${testLabel}</strong><br/>
            <span style="font-size: 11px; color: ${isDark ? '#9ca3af' : '#6b7280'}; word-break: break-all; display: block;">${item.testName}</span><br/>
            Throughput: ${item.throughput.toFixed(2)} MGas/s<br/>
            Execution: ${item.executionMs.toFixed(2)}ms<br/>
            <span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${CATEGORY_COLORS[item.category]};margin-right:6px;vertical-align:middle;"></span>${item.category.charAt(0).toUpperCase() + item.category.slice(1)}
          `
        },
      },
      legend: {
        data: ALL_CATEGORIES.map((c) => c.charAt(0).toUpperCase() + c.slice(1)),
        bottom: 0,
        textStyle: { color: textColor, fontSize: 11 },
        itemWidth: 10,
        itemHeight: 10,
        type: 'scroll',
      },
      grid: {
        left: 50,
        right: 20,
        top: 20,
        bottom: chartData.length > 50 ? 110 : 70,
      },
      xAxis: {
        type: 'category' as const,
        data: testLabels,
        axisLabel: {
          color: textColor,
          fontSize: 10,
          rotate: chartData.length > 50 ? 90 : 45,
          interval: chartData.length > 100 ? Math.floor(chartData.length / 50) : 0,
        },
        axisLine: { lineStyle: { color: gridColor } },
        axisTick: { show: false },
        name: sortMode === 'order' ? 'Test #' : 'Tests (sorted by throughput)',
        nameLocation: 'middle' as const,
        nameGap: chartData.length > 50 ? 60 : 30,
        nameTextStyle: { color: subTextColor, fontSize: 11 },
      },
      yAxis: {
        type: useLogScale ? ('log' as const) : ('value' as const),
        name: 'MGas/s',
        nameLocation: 'middle' as const,
        nameGap: 35,
        nameTextStyle: { color: subTextColor, fontSize: 11 },
        axisLabel: { color: textColor, fontSize: 11 },
        axisLine: { lineStyle: { color: gridColor } },
        splitLine: { lineStyle: { color: gridColor, type: 'dashed' as const } },
        min: useLogScale ? 1 : 0,
        scale: true,
      },
      dataZoom: [
        {
          type: 'inside' as const,
          xAxisIndex: 0,
          filterMode: 'filter' as const,
          start: 0,
          end: chartData.length > 50 ? (50 / chartData.length) * 100 : 100,
          zoomOnMouseWheel: true,
          moveOnMouseMove: true,
          moveOnMouseWheel: false,
        },
        ...(chartData.length > 50 ? [{
          type: 'slider' as const,
          xAxisIndex: 0,
          filterMode: 'filter' as const,
          height: 20,
          bottom: 40,
          start: 0,
          end: (50 / chartData.length) * 100,
          fillerColor: isDark ? 'rgba(59, 130, 246, 0.3)' : 'rgba(59, 130, 246, 0.2)',
          borderColor: gridColor,
          handleStyle: { color: '#3b82f6' },
        }] : []),
      ],
      series: [
        // Main bar series (no name so it doesn't appear in legend)
        {
          type: 'bar' as const,
          data: chartData.map((d) => ({
            value: d.throughput,
            itemStyle: { color: CATEGORY_COLORS[d.category] },
          })),
          barMaxWidth: 30,
        },
        // Dummy series for legend colors (using scatter to not affect bar width)
        ...ALL_CATEGORIES.map((category) => ({
          name: category.charAt(0).toUpperCase() + category.slice(1),
          type: 'scatter' as const,
          data: [],
          itemStyle: { color: CATEGORY_COLORS[category] },
        })),
      ],
    }
  }, [chartData, isDark, useLogScale, sortMode, textColor, subTextColor, gridColor, tooltipBg, tooltipBorder])

  const onEvents = useMemo(() => {
    if (!onTestClick) return undefined
    return {
      click: (params: { dataIndex: number }) => {
        const item = chartData[params.dataIndex]
        if (item) {
          onTestClick(item.testName)
        }
      },
    }
  }, [chartData, onTestClick])

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100">
          Throughput by Test
        </h4>
        <div className="flex items-center gap-1">
          <span className="text-xs text-gray-500 dark:text-gray-400">Sort:</span>
          <button
            onClick={() => setSortMode('order')}
            className={`px-2 py-0.5 text-xs rounded-sm ${
              sortMode === 'order'
                ? 'bg-blue-500 text-white'
                : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600'
            }`}
          >
            Test #
          </button>
          <button
            onClick={() => setSortMode('throughput')}
            className={`px-2 py-0.5 text-xs rounded-sm ${
              sortMode === 'throughput'
                ? 'bg-blue-500 text-white'
                : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600'
            }`}
          >
            Throughput
          </button>
        </div>
      </div>
      <ReactECharts
        option={option}
        style={{ height: '400px', width: '100%', cursor: onTestClick ? 'pointer' : 'default' }}
        opts={{ renderer: 'svg' }}
        onEvents={onEvents}
      />
    </div>
  )
}
