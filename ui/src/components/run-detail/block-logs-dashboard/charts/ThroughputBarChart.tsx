import { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'
import type { ProcessedTestData } from '../types'
import { CATEGORY_COLORS } from '../utils/colors'

interface ThroughputBarChartProps {
  data: ProcessedTestData[]
  isDark: boolean
  useLogScale: boolean
}

export function ThroughputBarChart({ data, isDark, useLogScale }: ThroughputBarChartProps) {
  const textColor = isDark ? '#e5e7eb' : '#374151'
  const subTextColor = isDark ? '#9ca3af' : '#6b7280'
  const gridColor = isDark ? '#374151' : '#e5e7eb'
  const tooltipBg = isDark ? '#1f2937' : '#ffffff'
  const tooltipBorder = isDark ? '#374151' : '#e5e7eb'

  // Sort by test order (ascending)
  const chartData = useMemo(() => {
    return [...data].sort((a, b) => a.testOrder - b.testOrder)
  }, [data])

  const option = useMemo(() => {
    // Use test order for X axis labels (e.g., "#1", "#2", etc.)
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
        formatter: (params: { name: string; value: number; dataIndex: number }[]) => {
          const param = params[0]
          const item = chartData[param.dataIndex]
          const testLabel = item.testOrder === Infinity ? '-' : `#${item.testOrder}`
          return `
            <div style="font-weight: 500; margin-bottom: 4px">${testLabel}: ${item.testName}</div>
            <div>Throughput: ${item.throughput.toFixed(2)} MGas/s</div>
            <div>Execution: ${item.executionMs.toFixed(2)}ms</div>
            <div>Category: ${item.category}</div>
          `
        },
      },
      grid: {
        left: 50,
        right: 20,
        top: 20,
        bottom: chartData.length > 30 ? 80 : 40,
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
        name: 'Test #',
        nameLocation: 'middle' as const,
        nameGap: chartData.length > 30 ? 60 : 30,
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
      dataZoom: chartData.length > 30 ? [
        {
          type: 'slider' as const,
          xAxisIndex: 0,
          filterMode: 'filter' as const,
          height: 20,
          bottom: 10,
          start: 0,
          end: (30 / chartData.length) * 100,
          fillerColor: isDark ? 'rgba(59, 130, 246, 0.3)' : 'rgba(59, 130, 246, 0.2)',
          borderColor: gridColor,
          handleStyle: { color: '#3b82f6' },
        },
      ] : undefined,
      series: [
        {
          type: 'bar' as const,
          data: chartData.map((d) => ({
            value: d.throughput,
            itemStyle: { color: CATEGORY_COLORS[d.category] },
          })),
          barMaxWidth: 30,
        },
      ],
    }
  }, [chartData, isDark, useLogScale, textColor, subTextColor, gridColor, tooltipBg, tooltipBorder])

  return (
    <div className="flex flex-col gap-2">
      <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100">
        Throughput by Test ({data.length} tests)
      </h4>
      <ReactECharts
        option={option}
        style={{ height: '400px', width: '100%' }}
        opts={{ renderer: 'svg' }}
      />
    </div>
  )
}
