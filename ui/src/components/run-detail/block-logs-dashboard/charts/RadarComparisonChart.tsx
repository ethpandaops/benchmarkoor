import { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'
import type { ProcessedTestData } from '../types'
import { COMPARISON_COLORS } from '../utils/colors'

interface RadarComparisonChartProps {
  selectedData: ProcessedTestData[]
  isDark: boolean
}

export function RadarComparisonChart({ selectedData, isDark }: RadarComparisonChartProps) {
  const textColor = isDark ? '#e5e7eb' : '#374151'
  const tooltipBg = isDark ? '#1f2937' : '#ffffff'
  const tooltipBorder = isDark ? '#374151' : '#e5e7eb'

  const option = useMemo(() => {
    const indicators = [
      { name: 'Throughput', max: 100 },
      { name: 'Speed', max: 100 },
      { name: 'Low Overhead', max: 100 },
      { name: 'Account Cache', max: 100 },
      { name: 'Code Cache', max: 100 },
    ]

    const series = selectedData.map((item, index) => ({
      value: [
        item.normalizedThroughput,
        item.normalizedSpeed,
        item.normalizedLowOverhead,
        item.normalizedAccountCache,
        item.normalizedCodeCache,
      ],
      name: item.testName.length > 30 ? item.testName.slice(0, 27) + '...' : item.testName,
      itemStyle: { color: COMPARISON_COLORS[index % COMPARISON_COLORS.length] },
      lineStyle: { color: COMPARISON_COLORS[index % COMPARISON_COLORS.length], width: 2 },
      areaStyle: {
        color: COMPARISON_COLORS[index % COMPARISON_COLORS.length],
        opacity: 0.1,
      },
    }))

    return {
      tooltip: {
        trigger: 'item' as const,
        backgroundColor: tooltipBg,
        borderColor: tooltipBorder,
        textStyle: { color: textColor },
      },
      legend: {
        data: series.map((s) => s.name),
        bottom: 0,
        textStyle: { color: textColor, fontSize: 10 },
        itemWidth: 10,
        itemHeight: 10,
        type: 'scroll' as const,
      },
      radar: {
        indicator: indicators,
        shape: 'polygon' as const,
        splitNumber: 5,
        axisName: {
          color: textColor,
          fontSize: 11,
        },
        splitLine: {
          lineStyle: {
            color: isDark ? '#374151' : '#e5e7eb',
          },
        },
        splitArea: {
          show: true,
          areaStyle: {
            color: isDark
              ? ['rgba(55, 65, 81, 0.3)', 'rgba(55, 65, 81, 0.1)']
              : ['rgba(229, 231, 235, 0.3)', 'rgba(229, 231, 235, 0.1)'],
          },
        },
        axisLine: {
          lineStyle: {
            color: isDark ? '#4b5563' : '#d1d5db',
          },
        },
      },
      series: [
        {
          type: 'radar' as const,
          data: series,
          emphasis: {
            lineStyle: { width: 3 },
            areaStyle: { opacity: 0.2 },
          },
        },
      ],
    }
  }, [selectedData, isDark, textColor, tooltipBg, tooltipBorder])

  return (
    <ReactECharts
      option={option}
      style={{ height: '350px', width: '100%' }}
      opts={{ renderer: 'svg' }}
    />
  )
}
