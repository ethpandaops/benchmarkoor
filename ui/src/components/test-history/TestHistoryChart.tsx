import { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'
import { getClientChartColor } from '@/utils/client-colors'
import { formatDuration } from '@/utils/format'
import { formatHistoryValue, HISTORY_METRIC_LABELS, type HistoryMetric, type HistoryPoint } from '@/utils/testHistory'

export type HistoryXAxis = 'time' | 'runCount'

interface TestHistoryChartProps {
  pointsByClient: Map<string, HistoryPoint[]>
  metric: HistoryMetric
  xAxis: HistoryXAxis
  isDark: boolean
  onRunClick: (runId: string) => void
}

// Above this many points per client, the chart hides the symbols and adds a
// zoom slider.
const LARGE_SERIES = 30

// [x, value, runId, image, runStart, runNumber]
type ChartDatum = [number, number, string, string, number, number]

function capitalizeFirst(str: string): string {
  return str ? str.charAt(0).toUpperCase() + str.slice(1) : str
}

export function TestHistoryChart({ pointsByClient, metric, xAxis, isDark, onRunClick }: TestHistoryChartProps) {
  const option = useMemo(() => {
    const textColor = isDark ? '#e5e7eb' : '#374151'
    const mutedColor = isDark ? '#9ca3af' : '#6b7280'
    const axisLineColor = isDark ? '#4b5563' : '#d1d5db'
    const splitLineColor = isDark ? '#374151' : '#e5e7eb'

    const maxSeriesLength = Math.max(0, ...[...pointsByClient.values()].map((p) => p.length))
    const isLarge = maxSeriesLength > LARGE_SERIES

    const series = [...pointsByClient.entries()].map(([client, points]) => ({
      name: capitalizeFirst(client),
      type: 'line' as const,
      // Run #1 is the most recent run of the client.
      data: points.map((p, i): ChartDatum => {
        const runNumber = points.length - i
        return [xAxis === 'time' ? p.runStart * 1000 : runNumber, p.value, p.runId, p.image ?? '', p.runStart, runNumber]
      }),
      showSymbol: !isLarge,
      symbolSize: isLarge ? 4 : 7,
      lineStyle: { width: isLarge ? 1.5 : 2 },
      itemStyle: { color: getClientChartColor(client) },
      emphasis: { focus: 'series' as const },
      cursor: 'pointer',
    }))

    const xAxisConfig =
      xAxis === 'time'
        ? {
            type: 'time' as const,
            axisLabel: {
              color: textColor,
              formatter: (value: number) => {
                const date = new Date(value)
                return `${date.getMonth() + 1}/${date.getDate()}`
              },
            },
          }
        : {
            type: 'value' as const,
            inverse: true,
            min: 1,
            max: Math.max(1, maxSeriesLength),
            minInterval: 1,
            axisLabel: { color: textColor, formatter: (value: number) => `#${value}` },
          }

    return {
      backgroundColor: 'transparent',
      tooltip: {
        trigger: 'item',
        appendToBody: true,
        backgroundColor: isDark ? '#1f2937' : '#ffffff',
        borderColor: isDark ? '#374151' : '#e5e7eb',
        textStyle: { color: textColor },
        formatter: (params: { seriesName: string; color: string; value: ChartDatum }) => {
          const [, value, runId, image, runStart, runNumber] = params.value
          let html = `<div style="display:flex;align-items:center;gap:6px;margin-bottom:4px;">`
          html += `<span style="display:inline-block;width:10px;height:10px;border-radius:50%;background-color:${params.color};"></span>`
          html += `<strong>${params.seriesName}</strong> · run #${runNumber}</div>`
          html += `<div style="margin-bottom:4px;"><strong>${formatHistoryValue(value, metric)}</strong></div>`
          html += `<div style="color:${mutedColor};font-size:11px;">${new Date(runStart * 1000).toLocaleString()}</div>`
          if (image) html += `<div style="color:${mutedColor};font-size:11px;">${image}</div>`
          html += `<div style="color:${mutedColor};font-size:11px;">${runId}</div>`
          html += `<div style="color:${isDark ? '#60a5fa' : '#3b82f6'};font-size:11px;margin-top:4px;">Click to open the run</div>`
          return html
        },
      },
      legend: {
        type: 'scroll',
        bottom: isLarge ? 30 : 0,
        textStyle: { color: textColor },
      },
      grid: {
        left: '3%',
        right: '4%',
        bottom: isLarge ? 64 : 40,
        top: 30,
        containLabel: true,
      },
      xAxis: {
        ...xAxisConfig,
        axisLine: { lineStyle: { color: axisLineColor } },
        splitLine: { show: false },
      },
      yAxis: {
        type: 'value',
        name: HISTORY_METRIC_LABELS[metric],
        nameTextStyle: { color: textColor },
        scale: true,
        axisLabel: {
          color: textColor,
          formatter: (value: number) => (metric === 'mgas' ? value.toFixed(0) : formatDuration(Math.round(value))),
        },
        axisLine: { lineStyle: { color: axisLineColor } },
        splitLine: { lineStyle: { color: splitLineColor } },
      },
      ...(isLarge && {
        dataZoom: [
          {
            type: 'slider' as const,
            xAxisIndex: 0,
            height: 20,
            bottom: 5,
            borderColor: axisLineColor,
            textStyle: { color: textColor },
          },
          { type: 'inside' as const, xAxisIndex: 0, zoomOnMouseWheel: true, moveOnMouseWheel: false },
        ],
      }),
      animation: !isLarge,
      series,
    }
  }, [pointsByClient, metric, xAxis, isDark])

  const handleClick = (params: { value?: ChartDatum }) => {
    const runId = params.value?.[2]
    if (runId) onRunClick(runId)
  }

  return (
    <ReactECharts
      option={option}
      notMerge
      style={{ height: '360px', width: '100%' }}
      opts={{ renderer: 'svg' }}
      onEvents={{ click: handleClick }}
    />
  )
}
