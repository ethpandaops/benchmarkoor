import { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'
import type { ProcessedTestData } from '../types'
import { CATEGORY_COLORS } from '../utils/colors'

interface ThroughputBarChartProps {
  data: ProcessedTestData[]
  isDark: boolean
  useLogScale: boolean
  maxItems?: number
}

export function ThroughputBarChart({ data, isDark, useLogScale, maxItems = 30 }: ThroughputBarChartProps) {
  const textColor = isDark ? '#e5e7eb' : '#374151'
  const subTextColor = isDark ? '#9ca3af' : '#6b7280'
  const gridColor = isDark ? '#374151' : '#e5e7eb'
  const tooltipBg = isDark ? '#1f2937' : '#ffffff'
  const tooltipBorder = isDark ? '#374151' : '#e5e7eb'

  // Sort by throughput descending, take top N, keep slowest at top
  const chartData = useMemo(() => {
    return [...data]
      .sort((a, b) => b.throughput - a.throughput)
      .slice(0, maxItems)
      // No reverse - slowest of the top N appears at top
  }, [data, maxItems])

  const option = useMemo(() => {
    // Use test order for Y axis labels (e.g., "#1", "#2", etc.)
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
        left: 60,
        right: 50,
        top: 20,
        bottom: 40,
      },
      xAxis: {
        type: useLogScale ? ('log' as const) : ('value' as const),
        name: 'MGas/s',
        nameLocation: 'middle' as const,
        nameGap: 25,
        nameTextStyle: { color: subTextColor, fontSize: 11 },
        axisLabel: { color: textColor, fontSize: 11 },
        axisLine: { lineStyle: { color: gridColor } },
        splitLine: { lineStyle: { color: gridColor, type: 'dashed' as const } },
        // Let ECharts auto-calculate based on visible data (filtered by dataZoom)
        min: useLogScale ? 1 : 0,
        scale: true, // Allow axis to not include zero and scale to data
      },
      yAxis: {
        type: 'category' as const,
        data: testLabels,
        axisLabel: {
          color: textColor,
          fontSize: 10,
        },
        axisLine: { lineStyle: { color: gridColor } },
        axisTick: { show: false },
      },
      dataZoom: chartData.length > 20 ? [
        {
          type: 'slider' as const,
          yAxisIndex: 0,
          // 'filter' mode: X axis will auto-adjust to only visible tests' throughput range
          filterMode: 'filter' as const,
          width: 20,
          right: 5,
          // Show top section (slowest tests) by default
          start: 100 - (20 / chartData.length) * 100,
          end: 100,
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
          barMaxWidth: 20,
          label: {
            show: true,
            position: 'right' as const,
            formatter: (params: { value: number }) => params.value.toFixed(1),
            color: textColor,
            fontSize: 10,
          },
        },
      ],
    }
  }, [chartData, isDark, useLogScale, textColor, subTextColor, gridColor, tooltipBg, tooltipBorder])

  const height = Math.max(300, Math.min(chartData.length * 25, 600))

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100">
          Throughput by Test (Top {Math.min(data.length, maxItems)})
        </h4>
        {data.length > maxItems && (
          <span className="text-xs text-gray-500 dark:text-gray-400">
            Showing {maxItems} of {data.length} tests
          </span>
        )}
      </div>
      <ReactECharts
        option={option}
        style={{ height: `${height}px`, width: '100%' }}
        opts={{ renderer: 'svg' }}
      />
    </div>
  )
}
