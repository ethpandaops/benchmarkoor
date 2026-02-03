import { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'
import type { ProcessedTestData } from '../types'
import { createHistogramBins } from '../utils/statistics'
import { ALL_CATEGORIES, CATEGORY_COLORS } from '../utils/colors'

interface HistogramChartProps {
  data: ProcessedTestData[]
  isDark: boolean
  useLogScale: boolean
}

export function HistogramChart({ data, isDark, useLogScale }: HistogramChartProps) {
  const textColor = isDark ? '#e5e7eb' : '#374151'
  const gridColor = isDark ? '#374151' : '#e5e7eb'
  const tooltipBg = isDark ? '#1f2937' : '#ffffff'
  const tooltipBorder = isDark ? '#374151' : '#e5e7eb'

  const bins = useMemo(() => createHistogramBins(data, 20), [data])

  const option = useMemo(() => {
    if (bins.length === 0) {
      return {}
    }

    const xAxisLabels = bins.map((b) => `${b.start.toFixed(0)}-${b.end.toFixed(0)}`)

    return {
      tooltip: {
        trigger: 'axis' as const,
        axisPointer: { type: 'shadow' as const },
        backgroundColor: tooltipBg,
        borderColor: tooltipBorder,
        textStyle: { color: textColor },
        formatter: (params: Array<{ seriesName: string; value: number; color: string; dataIndex: number }>) => {
          const bin = bins[params[0].dataIndex]
          let content = `<div style="font-weight: 500; margin-bottom: 4px">${bin.start.toFixed(1)} - ${bin.end.toFixed(1)} MGas/s</div>`
          content += `<div>Total: ${bin.count} tests</div>`
          content += '<hr style="margin: 4px 0; border-color: #666"/>'
          params.forEach((p) => {
            if (p.value > 0) {
              content += `<div style="display: flex; align-items: center; gap: 4px">
                <span style="display: inline-block; width: 10px; height: 10px; border-radius: 2px; background-color: ${p.color}"></span>
                ${p.seriesName}: ${p.value}
              </div>`
            }
          })
          return content
        },
      },
      legend: {
        data: ALL_CATEGORIES.map((c) => c.charAt(0).toUpperCase() + c.slice(1)),
        bottom: 0,
        textStyle: { color: textColor, fontSize: 11 },
        itemWidth: 12,
        itemHeight: 8,
        type: 'scroll',
      },
      grid: {
        left: 50,
        right: 30,
        top: 20,
        bottom: 50,
      },
      xAxis: {
        type: 'category' as const,
        data: xAxisLabels,
        axisLabel: {
          color: textColor,
          fontSize: 9,
          rotate: 45,
          interval: Math.floor(bins.length / 10),
        },
        axisLine: { lineStyle: { color: gridColor } },
        axisTick: { show: false },
        name: 'MGas/s',
        nameLocation: 'middle' as const,
        nameGap: 35,
        nameTextStyle: { color: textColor, fontSize: 11 },
      },
      yAxis: {
        type: useLogScale ? ('log' as const) : ('value' as const),
        name: 'Count',
        nameTextStyle: { color: textColor, fontSize: 11 },
        axisLabel: { color: textColor, fontSize: 11 },
        axisLine: { show: false },
        splitLine: { lineStyle: { color: gridColor, type: 'dashed' as const } },
        min: useLogScale ? 1 : 0,
      },
      series: ALL_CATEGORIES.map((category) => ({
        name: category.charAt(0).toUpperCase() + category.slice(1),
        type: 'bar' as const,
        stack: 'total',
        data: bins.map((b) => b.byCategory[category]),
        itemStyle: { color: CATEGORY_COLORS[category] },
        emphasis: { focus: 'series' as const },
      })),
    }
  }, [bins, useLogScale, textColor, gridColor, tooltipBg, tooltipBorder])

  if (bins.length === 0) {
    return (
      <div className="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
        Not enough data to generate histogram.
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100">
        Throughput Histogram
      </h4>
      <p className="text-xs text-gray-500 dark:text-gray-400">
        Distribution of tests by throughput range, stacked by category.
      </p>
      <ReactECharts
        option={option}
        style={{ height: '300px', width: '100%' }}
        opts={{ renderer: 'svg' }}
      />
    </div>
  )
}
