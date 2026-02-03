import { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'
import type { ProcessedTestData } from '../types'
import { ALL_CATEGORIES, CATEGORY_COLORS } from '../utils/colors'

interface ThroughputScatterChartProps {
  data: ProcessedTestData[]
  isDark: boolean
  useLogScale: boolean
  onTestClick?: (testName: string) => void
}

export function ThroughputScatterChart({ data, isDark, useLogScale, onTestClick }: ThroughputScatterChartProps) {
  const textColor = isDark ? '#e5e7eb' : '#374151'
  const subTextColor = isDark ? '#9ca3af' : '#6b7280'
  const gridColor = isDark ? '#374151' : '#e5e7eb'
  const tooltipBg = isDark ? '#1f2937' : '#ffffff'
  const tooltipBorder = isDark ? '#374151' : '#e5e7eb'

  const option = useMemo(() => {
    const seriesData = ALL_CATEGORIES.map((category) => ({
      name: category.charAt(0).toUpperCase() + category.slice(1),
      type: 'scatter' as const,
      data: data
        .filter((d) => d.category === category)
        .map((d) => ({
          value: [d.executionMs, d.throughput],
          testName: d.testName,
          item: d,
        })),
      itemStyle: { color: CATEGORY_COLORS[category] },
      symbolSize: 8,
      emphasis: {
        itemStyle: { borderColor: textColor, borderWidth: 2 },
        scale: 1.5,
      },
    }))

    return {
      tooltip: {
        trigger: 'item' as const,
        backgroundColor: tooltipBg,
        borderColor: tooltipBorder,
        textStyle: { color: textColor },
        extraCssText: 'max-width: 300px; white-space: normal;',
        formatter: (params: { data: { testName: string; item: ProcessedTestData } }) => {
          const item = params.data.item
          const testLabel = item.testOrder === Infinity ? '-' : `#${item.testOrder}`
          return `
            <strong>Test ${testLabel}</strong><br/>
            <span style="font-size: 11px; color: ${isDark ? '#9ca3af' : '#6b7280'}; word-break: break-all; display: block;">${item.testName}</span><br/>
            Throughput: ${item.throughput.toFixed(2)} MGas/s<br/>
            Execution: ${item.executionMs.toFixed(2)}ms<br/>
            Overhead: ${item.overheadMs.toFixed(2)}ms<br/>
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
        left: 60,
        right: 30,
        top: 20,
        bottom: 100,
      },
      xAxis: {
        type: useLogScale ? ('log' as const) : ('value' as const),
        name: 'Execution Time (ms)',
        nameLocation: 'middle' as const,
        nameGap: 25,
        nameTextStyle: { color: subTextColor, fontSize: 11 },
        axisLabel: { color: textColor, fontSize: 11 },
        axisLine: { lineStyle: { color: gridColor } },
        splitLine: { lineStyle: { color: gridColor, type: 'dashed' as const } },
        min: useLogScale ? 0.01 : undefined,
      },
      yAxis: {
        type: useLogScale ? ('log' as const) : ('value' as const),
        name: 'MGas/s',
        nameLocation: 'middle' as const,
        nameGap: 40,
        nameTextStyle: { color: subTextColor, fontSize: 11 },
        axisLabel: { color: textColor, fontSize: 11 },
        axisLine: { lineStyle: { color: gridColor } },
        splitLine: { lineStyle: { color: gridColor, type: 'dashed' as const } },
        min: useLogScale ? 1 : undefined,
      },
      dataZoom: [
        {
          type: 'inside' as const,
          xAxisIndex: 0,
          zoomOnMouseWheel: true,
          moveOnMouseMove: true,
          moveOnMouseWheel: false,
        },
        {
          type: 'slider' as const,
          xAxisIndex: 0,
          height: 20,
          bottom: 45,
          fillerColor: isDark ? 'rgba(59, 130, 246, 0.3)' : 'rgba(59, 130, 246, 0.2)',
          borderColor: gridColor,
          handleStyle: { color: '#3b82f6' },
        },
      ],
      series: seriesData,
    }
  }, [data, isDark, useLogScale, textColor, subTextColor, gridColor, tooltipBg, tooltipBorder])

  const onEvents = useMemo(() => {
    if (!onTestClick) return undefined
    return {
      click: (params: { data: { testName: string } }) => {
        if (params.data?.testName) {
          onTestClick(params.data.testName)
        }
      },
    }
  }, [onTestClick])

  return (
    <div className="flex flex-col gap-2">
      <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100">
        Execution Time vs Throughput
      </h4>
      <ReactECharts
        option={option}
        style={{ height: '400px', width: '100%', cursor: onTestClick ? 'pointer' : 'default' }}
        opts={{ renderer: 'svg' }}
        onEvents={onEvents}
      />
    </div>
  )
}
