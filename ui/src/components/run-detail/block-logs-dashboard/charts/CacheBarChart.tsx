import { useMemo, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import type { ProcessedTestData, TestCategory } from '../types'
import { ALL_CATEGORIES, CATEGORY_COLORS, CACHE_COLORS } from '../utils/colors'

type SortMode = 'order' | 'hitRate' | 'total'
type ViewMode = 'percent' | 'stacked'
type CacheType = 'account' | 'storage' | 'code'

interface CacheBarChartProps {
  cacheType: CacheType
  data: ProcessedTestData[]
  isDark: boolean
  activeCategories?: TestCategory[]
  onTestClick?: (testName: string) => void
}

const CACHE_TYPE_LABELS: Record<CacheType, string> = {
  account: 'Account Cache',
  storage: 'Storage Cache',
  code: 'Code Cache',
}

function getCacheData(item: ProcessedTestData, cacheType: CacheType) {
  switch (cacheType) {
    case 'account':
      return {
        hitRate: item.accountCacheHitRate,
        hits: item.accountCacheHits,
        misses: item.accountCacheMisses,
      }
    case 'storage':
      return {
        hitRate: item.storageCacheHitRate,
        hits: item.storageCacheHits,
        misses: item.storageCacheMisses,
      }
    case 'code':
      return {
        hitRate: item.codeCacheHitRate,
        hits: item.codeCacheHits,
        misses: item.codeCacheMisses,
      }
  }
}

export function CacheBarChart({
  cacheType,
  data,
  isDark,
  activeCategories,
  onTestClick,
}: CacheBarChartProps) {
  const categoriesToShow = activeCategories ?? ALL_CATEGORIES
  const [sortMode, setSortMode] = useState<SortMode>('hitRate')
  const [viewMode, setViewMode] = useState<ViewMode>('percent')

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
      const aCache = getCacheData(a, cacheType)
      const bCache = getCacheData(b, cacheType)
      if (sortMode === 'total') {
        return (aCache.hits + aCache.misses) - (bCache.hits + bCache.misses)
      }
      return aCache.hitRate - bCache.hitRate
    })
  }, [data, sortMode, cacheType])

  const option = useMemo(() => {
    const testLabels = chartData.map((d) =>
      d.testOrder === Infinity ? '-' : `#${d.testOrder}`
    )

    const baseOption = {
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
          const cache = getCacheData(item, cacheType)
          const testLabel = item.testOrder === Infinity ? '-' : `#${item.testOrder}`
          const total = cache.hits + cache.misses
          return `
            <strong>Test ${testLabel}</strong><br/>
            <span style="font-size: 11px; color: ${isDark ? '#9ca3af' : '#6b7280'}; word-break: break-all; display: block;">${item.testName}</span><br/>
            Hit Rate: ${cache.hitRate.toFixed(1)}%<br/>
            Hits: ${cache.hits.toLocaleString()}<br/>
            Misses: ${cache.misses.toLocaleString()}<br/>
            Total: ${total.toLocaleString()}<br/>
            <span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${CATEGORY_COLORS[item.category]};margin-right:6px;vertical-align:middle;"></span>${item.category.charAt(0).toUpperCase() + item.category.slice(1)}
          `
        },
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
        name: sortMode === 'order' ? 'Test #' : sortMode === 'hitRate' ? 'Tests (sorted by hit rate)' : 'Tests (sorted by total accesses)',
        nameLocation: 'middle' as const,
        nameGap: chartData.length > 50 ? 60 : 30,
        nameTextStyle: { color: subTextColor, fontSize: 11 },
      },
      dataZoom: [
        {
          type: 'inside' as const,
          xAxisIndex: 0,
          filterMode: 'filter' as const,
          start: 0,
          end: 100,
          zoomOnMouseWheel: true,
          moveOnMouseMove: true,
          moveOnMouseWheel: false,
        },
        ...(chartData.length > 50
          ? [
              {
                type: 'slider' as const,
                xAxisIndex: 0,
                filterMode: 'filter' as const,
                height: 20,
                bottom: 40,
                start: 0,
                end: 100,
                fillerColor: isDark
                  ? 'rgba(59, 130, 246, 0.3)'
                  : 'rgba(59, 130, 246, 0.2)',
                borderColor: gridColor,
                handleStyle: { color: '#3b82f6' },
              },
            ]
          : []),
      ],
    }

    if (viewMode === 'percent') {
      return {
        ...baseOption,
        legend: {
          data: categoriesToShow.map((c) => c.charAt(0).toUpperCase() + c.slice(1)),
          bottom: 0,
          textStyle: { color: textColor, fontSize: 11 },
          itemWidth: 10,
          itemHeight: 10,
          type: 'scroll',
        },
        yAxis: {
          type: 'value' as const,
          name: 'Hit Rate %',
          nameLocation: 'middle' as const,
          nameGap: 35,
          nameTextStyle: { color: subTextColor, fontSize: 11 },
          axisLabel: { color: textColor, fontSize: 11 },
          axisLine: { lineStyle: { color: gridColor } },
          splitLine: { lineStyle: { color: gridColor, type: 'dashed' as const } },
          min: 0,
          max: 100,
        },
        series: [
          {
            type: 'bar' as const,
            data: chartData.map((d) => {
              const cache = getCacheData(d, cacheType)
              return {
                value: cache.hitRate,
                itemStyle: { color: CATEGORY_COLORS[d.category] },
              }
            }),
            barMaxWidth: 30,
          },
          ...categoriesToShow.map((category) => ({
            name: category.charAt(0).toUpperCase() + category.slice(1),
            type: 'scatter' as const,
            data: [],
            itemStyle: { color: CATEGORY_COLORS[category] },
          })),
        ],
      }
    } else {
      return {
        ...baseOption,
        legend: {
          data: ['Hits', 'Misses'],
          bottom: 0,
          textStyle: { color: textColor, fontSize: 11 },
          itemWidth: 10,
          itemHeight: 10,
        },
        yAxis: {
          type: 'value' as const,
          name: 'Count',
          nameLocation: 'middle' as const,
          nameGap: 35,
          nameTextStyle: { color: subTextColor, fontSize: 11 },
          axisLabel: { color: textColor, fontSize: 11 },
          axisLine: { lineStyle: { color: gridColor } },
          splitLine: { lineStyle: { color: gridColor, type: 'dashed' as const } },
        },
        series: [
          {
            name: 'Hits',
            type: 'bar' as const,
            stack: 'cache',
            data: chartData.map((d) => {
              const cache = getCacheData(d, cacheType)
              return cache.hits
            }),
            itemStyle: { color: CACHE_COLORS.hit },
            barMaxWidth: 30,
          },
          {
            name: 'Misses',
            type: 'bar' as const,
            stack: 'cache',
            data: chartData.map((d) => {
              const cache = getCacheData(d, cacheType)
              return cache.misses
            }),
            itemStyle: { color: CACHE_COLORS.miss },
            barMaxWidth: 30,
          },
        ],
      }
    }
  }, [
    chartData,
    isDark,
    viewMode,
    cacheType,
    textColor,
    subTextColor,
    gridColor,
    tooltipBg,
    tooltipBorder,
    categoriesToShow,
    sortMode,
  ])

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
          {CACHE_TYPE_LABELS[cacheType]} Hit Rate
        </h4>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-1">
            <span className="text-xs text-gray-500 dark:text-gray-400">View:</span>
            <button
              onClick={() => setViewMode('percent')}
              className={`px-2 py-0.5 text-xs rounded-xs ${
                viewMode === 'percent'
                  ? 'bg-blue-500 text-white'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600'
              }`}
            >
              %
            </button>
            <button
              onClick={() => setViewMode('stacked')}
              className={`px-2 py-0.5 text-xs rounded-xs ${
                viewMode === 'stacked'
                  ? 'bg-blue-500 text-white'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600'
              }`}
            >
              Hits/Misses
            </button>
          </div>
          <div className="flex items-center gap-1">
            <span className="text-xs text-gray-500 dark:text-gray-400">Sort:</span>
            <button
              onClick={() => setSortMode('order')}
              className={`px-2 py-0.5 text-xs rounded-xs ${
                sortMode === 'order'
                  ? 'bg-blue-500 text-white'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600'
              }`}
            >
              Test #
            </button>
            <button
              onClick={() => setSortMode('hitRate')}
              className={`px-2 py-0.5 text-xs rounded-xs ${
                sortMode === 'hitRate'
                  ? 'bg-blue-500 text-white'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600'
              }`}
            >
              Hit Rate
            </button>
            <button
              onClick={() => setSortMode('total')}
              className={`px-2 py-0.5 text-xs rounded-xs ${
                sortMode === 'total'
                  ? 'bg-blue-500 text-white'
                  : 'bg-gray-100 text-gray-600 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600'
              }`}
            >
              Total
            </button>
          </div>
        </div>
      </div>
      <ReactECharts
        option={option}
        notMerge={true}
        style={{ height: '400px', width: '100%', cursor: onTestClick ? 'pointer' : 'default' }}
        opts={{ renderer: 'svg' }}
        onEvents={onEvents}
      />
    </div>
  )
}
