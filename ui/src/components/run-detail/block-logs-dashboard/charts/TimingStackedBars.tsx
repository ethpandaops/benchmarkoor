import { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'
import type { ProcessedTestData } from '../types'
import { TIMING_COLORS } from '../utils/colors'

interface TimingStackedBarsProps {
  selectedData: ProcessedTestData[]
  isDark: boolean
}

export function TimingStackedBars({ selectedData, isDark }: TimingStackedBarsProps) {
  const textColor = isDark ? '#e5e7eb' : '#374151'
  const gridColor = isDark ? '#374151' : '#e5e7eb'
  const tooltipBg = isDark ? '#1f2937' : '#ffffff'
  const tooltipBorder = isDark ? '#374151' : '#e5e7eb'

  const option = useMemo(() => {
    // Use test order for X axis labels
    const testLabels = selectedData.map((d) =>
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
        formatter: (params: Array<{ seriesName: string; value: number; color: string; dataIndex: number }>) => {
          const item = selectedData[params[0].dataIndex]
          const testLabel = item.testOrder === Infinity ? '-' : `#${item.testOrder}`
          let content = `<strong>Test ${testLabel}</strong><br/>`
          content += `<span style="font-size: 11px; color: ${isDark ? '#9ca3af' : '#6b7280'}; word-break: break-all; display: block;">${item.testName}</span><br/>`
          content += `Total: ${item.totalMs.toFixed(2)}ms`
          content += '<hr style="margin: 4px 0; border-color: #666"/>'
          params.forEach((p) => {
            content += `<div style="display: flex; align-items: center; gap: 4px">
              <span style="display: inline-block; width: 10px; height: 10px; border-radius: 2px; background-color: ${p.color}"></span>
              ${p.seriesName}: ${p.value.toFixed(2)}ms (${((p.value / item.totalMs) * 100).toFixed(1)}%)
            </div>`
          })
          return content
        },
      },
      legend: {
        data: ['Execution', 'State Read', 'State Hash', 'Commit'],
        bottom: 0,
        textStyle: { color: textColor, fontSize: 11 },
        itemWidth: 12,
        itemHeight: 8,
      },
      grid: {
        left: 30,
        right: 30,
        top: 20,
        bottom: 50,
        containLabel: true,
      },
      xAxis: {
        type: 'category' as const,
        data: testLabels,
        axisLabel: {
          color: textColor,
          fontSize: 10,
          interval: 0,
        },
        axisLine: { lineStyle: { color: gridColor } },
        axisTick: { show: false },
      },
      yAxis: {
        type: 'value' as const,
        name: 'Time (ms)',
        nameTextStyle: { color: textColor, fontSize: 11 },
        axisLabel: { color: textColor, fontSize: 11 },
        axisLine: { show: false },
        splitLine: { lineStyle: { color: gridColor, type: 'dashed' as const } },
      },
      series: [
        {
          name: 'Execution',
          type: 'bar' as const,
          stack: 'total',
          data: selectedData.map((d) => d.executionMs),
          itemStyle: { color: TIMING_COLORS.execution },
          emphasis: { focus: 'series' as const },
        },
        {
          name: 'State Read',
          type: 'bar' as const,
          stack: 'total',
          data: selectedData.map((d) => d.stateReadMs),
          itemStyle: { color: TIMING_COLORS.stateRead },
          emphasis: { focus: 'series' as const },
        },
        {
          name: 'State Hash',
          type: 'bar' as const,
          stack: 'total',
          data: selectedData.map((d) => d.stateHashMs),
          itemStyle: { color: TIMING_COLORS.stateHash },
          emphasis: { focus: 'series' as const },
        },
        {
          name: 'Commit',
          type: 'bar' as const,
          stack: 'total',
          data: selectedData.map((d) => d.commitMs),
          itemStyle: { color: TIMING_COLORS.commit },
          emphasis: { focus: 'series' as const },
        },
      ],
    }
  }, [selectedData, isDark, textColor, gridColor, tooltipBg, tooltipBorder])

  return (
    <ReactECharts
      option={option}
      style={{ height: '300px', width: '100%' }}
      opts={{ renderer: 'svg' }}
    />
  )
}
