import { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'
import type { ProcessedTestData } from '../types'
import { ALL_CATEGORIES, CATEGORY_COLORS } from '../utils/colors'

interface CacheScatterChartProps {
  data: ProcessedTestData[]
  isDark: boolean
}

export function CacheScatterChart({ data, isDark }: CacheScatterChartProps) {
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
          value: [d.accountCacheHitRate, d.codeCacheHitRate],
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
        formatter: (params: { data: { testName: string; item: ProcessedTestData } }) => {
          const item = params.data.item
          const testLabel = item.testOrder === Infinity ? '-' : `#${item.testOrder}`
          return `
            <div style="font-weight: 500; margin-bottom: 4px; max-width: 300px; word-wrap: break-word">${testLabel}: ${item.testName}</div>
            <div>Account Cache: ${item.accountCacheHitRate.toFixed(1)}%</div>
            <div>Storage Cache: ${item.storageCacheHitRate.toFixed(1)}%</div>
            <div>Code Cache: ${item.codeCacheHitRate.toFixed(1)}%</div>
            <div>Category: ${item.category}</div>
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
        bottom: 50,
      },
      xAxis: {
        type: 'value' as const,
        name: 'Account Cache Hit Rate (%)',
        nameLocation: 'middle' as const,
        nameGap: 25,
        nameTextStyle: { color: subTextColor, fontSize: 11 },
        axisLabel: { color: textColor, fontSize: 11 },
        axisLine: { lineStyle: { color: gridColor } },
        splitLine: { lineStyle: { color: gridColor, type: 'dashed' as const } },
        min: 0,
        max: 100,
      },
      yAxis: {
        type: 'value' as const,
        name: 'Code Cache Hit Rate (%)',
        nameLocation: 'middle' as const,
        nameGap: 40,
        nameTextStyle: { color: subTextColor, fontSize: 11 },
        axisLabel: { color: textColor, fontSize: 11 },
        axisLine: { lineStyle: { color: gridColor } },
        splitLine: { lineStyle: { color: gridColor, type: 'dashed' as const } },
        min: 0,
        max: 100,
      },
      series: [
        ...seriesData,
        // Reference lines at 80%
        {
          name: 'Threshold',
          type: 'line' as const,
          markLine: {
            silent: true,
            lineStyle: { color: '#ef4444', type: 'dashed' as const, width: 1 },
            data: [
              { xAxis: 80 },
              { yAxis: 80 },
            ],
            label: {
              show: false,
            },
          },
          data: [],
        },
      ],
    }
  }, [data, isDark, textColor, subTextColor, gridColor, tooltipBg, tooltipBorder])

  return (
    <div className="flex flex-col gap-2">
      <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100">
        Account vs Code Cache Hit Rates
      </h4>
      <p className="text-xs text-gray-500 dark:text-gray-400">
        Red dashed lines indicate 80% threshold. Top-right quadrant = optimal caching.
      </p>
      <ReactECharts
        option={option}
        style={{ height: '350px', width: '100%' }}
        opts={{ renderer: 'svg' }}
      />
    </div>
  )
}
