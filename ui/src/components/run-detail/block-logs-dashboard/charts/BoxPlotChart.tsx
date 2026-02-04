import { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'
import type { ProcessedTestData } from '../types'
import { calculateBoxPlotStats } from '../utils/statistics'
import { CATEGORY_COLORS } from '../utils/colors'

interface BoxPlotChartProps {
  data: ProcessedTestData[]
  isDark: boolean
  useLogScale: boolean
}

export function BoxPlotChart({ data, isDark, useLogScale }: BoxPlotChartProps) {
  const textColor = isDark ? '#e5e7eb' : '#374151'
  const gridColor = isDark ? '#374151' : '#e5e7eb'
  const tooltipBg = isDark ? '#1f2937' : '#ffffff'
  const tooltipBorder = isDark ? '#374151' : '#e5e7eb'

  const boxPlotStats = useMemo(() => calculateBoxPlotStats(data), [data])

  const option = useMemo(() => {
    if (boxPlotStats.length === 0) {
      return {}
    }

    const categories = boxPlotStats.map((s) => s.category.charAt(0).toUpperCase() + s.category.slice(1))

    // boxplot data: [min, Q1, median, Q3, max]
    const boxData = boxPlotStats.map((s) => [s.min, s.q1, s.median, s.q3, s.max])

    // outliers: [[categoryIndex, value], ...]
    const outlierData: [number, number][] = []
    boxPlotStats.forEach((s, index) => {
      s.outliers.forEach((value) => {
        outlierData.push([index, value])
      })
    })

    return {
      tooltip: {
        trigger: 'item' as const,
        backgroundColor: tooltipBg,
        borderColor: tooltipBorder,
        textStyle: { color: textColor },
        formatter: (params: { componentType: string; name: string; data: number[] | [number, number]; seriesName: string }) => {
          if (params.componentType === 'series' && params.seriesName === 'outliers') {
            const [catIndex, value] = params.data as [number, number]
            return `${categories[catIndex]} Outlier: ${value.toFixed(2)} MGas/s`
          }
          if (params.componentType === 'series' && params.data) {
            const [min, q1, median, q3, max] = params.data as number[]
            return `
              <div style="font-weight: 500; margin-bottom: 4px">${params.name}</div>
              <div>Max: ${max.toFixed(2)}</div>
              <div>Q3: ${q3.toFixed(2)}</div>
              <div>Median: ${median.toFixed(2)}</div>
              <div>Q1: ${q1.toFixed(2)}</div>
              <div>Min: ${min.toFixed(2)}</div>
            `
          }
          return ''
        },
      },
      grid: {
        left: 60,
        right: 30,
        top: 20,
        bottom: 40,
      },
      xAxis: {
        type: 'category' as const,
        data: categories,
        axisLabel: { color: textColor, fontSize: 11 },
        axisLine: { lineStyle: { color: gridColor } },
        axisTick: { show: false },
      },
      yAxis: {
        type: useLogScale ? ('log' as const) : ('value' as const),
        name: 'MGas/s',
        nameTextStyle: { color: textColor, fontSize: 11 },
        axisLabel: { color: textColor, fontSize: 11 },
        axisLine: { show: false },
        splitLine: { lineStyle: { color: gridColor, type: 'dashed' as const } },
        min: useLogScale ? 1 : undefined,
      },
      series: [
        {
          name: 'boxplot',
          type: 'boxplot' as const,
          data: boxData.map((d, i) => ({
            value: d,
            itemStyle: {
              color: `${CATEGORY_COLORS[boxPlotStats[i].category]}20`,
              borderColor: CATEGORY_COLORS[boxPlotStats[i].category],
              borderWidth: 2,
            },
          })),
          boxWidth: ['40%', '60%'],
        },
        {
          name: 'outliers',
          type: 'scatter' as const,
          data: outlierData.map(([catIndex, value]) => ({
            value: [catIndex, value],
            itemStyle: { color: CATEGORY_COLORS[boxPlotStats[catIndex].category] },
          })),
          symbolSize: 6,
        },
      ],
    }
  }, [boxPlotStats, useLogScale, textColor, gridColor, tooltipBg, tooltipBorder])

  if (boxPlotStats.length === 0) {
    return (
      <div className="py-8 text-center text-sm text-gray-500 dark:text-gray-400">
        Not enough data to generate box plots.
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100">
        Throughput Distribution by Category
      </h4>
      <p className="text-xs text-gray-500 dark:text-gray-400">
        Box shows Q1-Q3 range with median. Whiskers extend to min/max (excluding outliers).
      </p>
      <ReactECharts
        option={option}
        style={{ height: '300px', width: '100%' }}
        opts={{ renderer: 'svg' }}
      />
    </div>
  )
}
