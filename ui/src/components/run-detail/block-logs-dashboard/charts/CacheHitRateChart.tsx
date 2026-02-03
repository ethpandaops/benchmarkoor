import { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'
import type { ProcessedTestData } from '../types'
import { CACHE_COLORS } from '../utils/colors'

interface CacheHitRateChartProps {
  data: ProcessedTestData[]
  isDark: boolean
  maxItems?: number
}

export function CacheHitRateChart({ data, isDark, maxItems = 20 }: CacheHitRateChartProps) {
  const textColor = isDark ? '#e5e7eb' : '#374151'
  const gridColor = isDark ? '#374151' : '#e5e7eb'
  const tooltipBg = isDark ? '#1f2937' : '#ffffff'
  const tooltipBorder = isDark ? '#374151' : '#e5e7eb'

  // Sort by average cache hit rate and take bottom N (worst performers)
  const chartData = useMemo(() => {
    return [...data]
      .map((d) => ({
        ...d,
        avgCacheHitRate: (d.accountCacheHitRate + d.storageCacheHitRate + d.codeCacheHitRate) / 3,
      }))
      .sort((a, b) => a.avgCacheHitRate - b.avgCacheHitRate)
      .slice(0, maxItems)
  }, [data, maxItems])

  const option = useMemo(() => {
    const testNames = chartData.map((d) =>
      d.testName.length > 30 ? d.testName.slice(0, 27) + '...' : d.testName
    )

    return {
      tooltip: {
        trigger: 'axis' as const,
        axisPointer: { type: 'shadow' as const },
        backgroundColor: tooltipBg,
        borderColor: tooltipBorder,
        textStyle: { color: textColor },
        formatter: (params: Array<{ seriesName: string; value: number; color: string; dataIndex: number }>) => {
          const item = chartData[params[0].dataIndex]
          let content = `<div style="font-weight: 500; margin-bottom: 4px">${item.testName}</div>`
          params.forEach((p) => {
            const isGood = p.value >= 80
            content += `<div style="display: flex; align-items: center; gap: 4px">
              <span style="display: inline-block; width: 10px; height: 10px; border-radius: 2px; background-color: ${p.color}"></span>
              ${p.seriesName}: ${p.value.toFixed(1)}% ${isGood ? '✓' : '⚠'}
            </div>`
          })
          return content
        },
      },
      legend: {
        data: ['Account', 'Storage', 'Code'],
        bottom: 0,
        textStyle: { color: textColor, fontSize: 11 },
        itemWidth: 12,
        itemHeight: 8,
      },
      grid: {
        left: 200,
        right: 30,
        top: 20,
        bottom: 50,
      },
      xAxis: {
        type: 'value' as const,
        name: 'Hit Rate (%)',
        nameLocation: 'middle' as const,
        nameGap: 25,
        nameTextStyle: { color: textColor, fontSize: 11 },
        axisLabel: { color: textColor, fontSize: 11 },
        axisLine: { lineStyle: { color: gridColor } },
        splitLine: { lineStyle: { color: gridColor, type: 'dashed' as const } },
        min: 0,
        max: 100,
      },
      yAxis: {
        type: 'category' as const,
        data: testNames,
        axisLabel: {
          color: textColor,
          fontSize: 10,
          width: 180,
          overflow: 'truncate' as const,
        },
        axisLine: { lineStyle: { color: gridColor } },
        axisTick: { show: false },
      },
      series: [
        {
          name: 'Account',
          type: 'bar' as const,
          data: chartData.map((d) => ({
            value: d.accountCacheHitRate,
            itemStyle: {
              color: d.accountCacheHitRate >= 80 ? CACHE_COLORS.good : CACHE_COLORS.poor,
            },
          })),
          barGap: '10%',
          barMaxWidth: 8,
        },
        {
          name: 'Storage',
          type: 'bar' as const,
          data: chartData.map((d) => ({
            value: d.storageCacheHitRate,
            itemStyle: {
              color: d.storageCacheHitRate >= 80 ? CACHE_COLORS.good : CACHE_COLORS.poor,
            },
          })),
          barMaxWidth: 8,
        },
        {
          name: 'Code',
          type: 'bar' as const,
          data: chartData.map((d) => ({
            value: d.codeCacheHitRate,
            itemStyle: {
              color: d.codeCacheHitRate >= 80 ? CACHE_COLORS.good : CACHE_COLORS.poor,
            },
          })),
          barMaxWidth: 8,
        },
      ],
      // 80% threshold line
      markLine: {
        silent: true,
        lineStyle: { color: '#ef4444', type: 'dashed' as const, width: 1 },
        data: [{ xAxis: 80 }],
        label: {
          formatter: '80%',
          position: 'end' as const,
          color: '#ef4444',
          fontSize: 10,
        },
      },
    }
  }, [chartData, isDark, textColor, gridColor, tooltipBg, tooltipBorder])

  const height = Math.max(300, Math.min(chartData.length * 35, 500))

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <h4 className="text-sm font-medium text-gray-900 dark:text-gray-100">
          Cache Hit Rates (Lowest Performers)
        </h4>
        <div className="flex items-center gap-3 text-xs">
          <span className="flex items-center gap-1">
            <span className="size-2.5 rounded-full" style={{ backgroundColor: CACHE_COLORS.good }} />
            ≥80%
          </span>
          <span className="flex items-center gap-1">
            <span className="size-2.5 rounded-full" style={{ backgroundColor: CACHE_COLORS.poor }} />
            &lt;80%
          </span>
        </div>
      </div>
      <ReactECharts
        option={option}
        style={{ height: `${height}px`, width: '100%' }}
        opts={{ renderer: 'svg' }}
      />
    </div>
  )
}
