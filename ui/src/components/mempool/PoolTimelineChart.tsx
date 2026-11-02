import { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'
import type { MempoolEvent, MempoolTimelinePoint } from '@/api/types'
import { formatBytes } from '@/utils/format'
import { splitEventMarkers } from './mempoolUtils'
import { chartTheme, SERIES_COLORS, useIsDark } from './chartTheme'

interface PoolTimelineChartProps {
  timeline: MempoolTimelinePoint[]
  events: MempoolEvent[]
}

function formatSeconds(ms: number): string {
  return `${(ms / 1000).toFixed(1)}s`
}

// PoolTimelineChart draws the pending and queued pool sizes over the
// scenario time, with a vertical line per event. Client (container) memory, when present,
// goes in a second plot below with its own scale (no dual axis).
export function PoolTimelineChart({ timeline, events }: PoolTimelineChartProps) {
  const isDark = useIsDark()
  const hasMemory = timeline.some((p) => p.memory_bytes !== undefined)
  const { markers, collapsed } = useMemo(() => splitEventMarkers(events), [events])

  const option = useMemo(() => {
    const t = chartTheme(isDark)
    const maxT = timeline.length > 0 ? timeline[timeline.length - 1].t_ms : 0
    const xAxisBase = {
      type: 'value' as const,
      min: 0,
      max: maxT,
      axisLabel: { color: t.muted, fontSize: 11, formatter: formatSeconds },
      axisLine: { lineStyle: { color: t.axisLine } },
      splitLine: { show: false },
    }
    const yAxisBase = {
      type: 'value' as const,
      axisLabel: { color: t.muted, fontSize: 11 },
      splitLine: { lineStyle: { color: t.splitLine } },
    }

    const grids = hasMemory
      ? [
          { left: 56, right: 16, top: 32, height: 150 },
          { left: 56, right: 16, top: 222, height: 70 },
        ]
      : [{ left: 56, right: 16, top: 32, bottom: 28 }]

    const eventLine = {
      symbol: 'none',
      silent: false,
      lineStyle: { type: 'dashed' as const, color: t.muted, width: 1 },
      label: { show: true, position: 'insideEndTop' as const, color: t.muted, fontSize: 10 },
      data: markers.map((e) => ({
        xAxis: e.t_ms,
        name: e.detail ? `${e.kind}: ${e.detail}` : e.kind,
        label: { formatter: e.kind },
      })),
    }

    const series: object[] = [
      {
        name: 'Pending',
        type: 'line',
        showSymbol: false,
        lineStyle: { width: 2 },
        itemStyle: { color: SERIES_COLORS.pending },
        data: timeline.map((p) => [p.t_ms, p.pending]),
        markLine: eventLine,
      },
      {
        name: 'Queued',
        type: 'line',
        showSymbol: false,
        lineStyle: { width: 2 },
        itemStyle: { color: SERIES_COLORS.queued },
        data: timeline.map((p) => [p.t_ms, p.queued]),
      },
    ]
    if (hasMemory) {
      series.push({
        name: 'Client memory',
        type: 'line',
        xAxisIndex: 1,
        yAxisIndex: 1,
        showSymbol: false,
        lineStyle: { width: 2 },
        itemStyle: { color: SERIES_COLORS.memory },
        areaStyle: { opacity: 0.1, color: SERIES_COLORS.memory },
        data: timeline.filter((p) => p.memory_bytes !== undefined).map((p) => [p.t_ms, p.memory_bytes]),
      })
    }

    return {
      backgroundColor: 'transparent',
      textStyle: { color: t.text },
      animation: false,
      legend: { top: 0, left: 'center', textStyle: { color: t.text, fontSize: 11 }, itemHeight: 8 },
      tooltip: {
        trigger: 'axis',
        backgroundColor: t.tooltipBg,
        borderColor: t.tooltipBorder,
        textStyle: { color: t.text, fontSize: 12 },
        axisPointer: { type: 'line', link: [{ xAxisIndex: 'all' }] },
        formatter: (params: Array<{ seriesName: string; color: string; value: [number, number] }>) => {
          if (!params.length) return ''
          let html = `<strong>${formatSeconds(params[0].value[0])}</strong><br/>`
          for (const p of params) {
            const v = p.seriesName === 'Client memory' ? formatBytes(p.value[1]) : p.value[1].toLocaleString()
            html += `<span style="display:inline-block;width:8px;height:8px;border-radius:50%;background-color:${p.color};margin-right:6px;"></span>${p.seriesName}: ${v}<br/>`
          }
          return html
        },
      },
      axisPointer: { link: [{ xAxisIndex: 'all' }] },
      grid: grids,
      xAxis: hasMemory
        ? [{ ...xAxisBase, gridIndex: 0, axisLabel: { show: false } }, { ...xAxisBase, gridIndex: 1 }]
        : [xAxisBase],
      yAxis: hasMemory
        ? [
            { ...yAxisBase, gridIndex: 0, name: 'txs', nameTextStyle: { color: t.muted, fontSize: 10 } },
            { ...yAxisBase, gridIndex: 1, splitNumber: 2, axisLabel: { ...yAxisBase.axisLabel, formatter: (v: number) => formatBytes(v) } },
          ]
        : [{ ...yAxisBase, name: 'txs', nameTextStyle: { color: t.muted, fontSize: 10 } }],
      series,
    }
  }, [isDark, timeline, markers, hasMemory])

  if (timeline.length === 0) {
    return <p className="text-sm/6 text-gray-500 dark:text-gray-400">No pool timeline recorded.</p>
  }

  return (
    <div className="flex flex-col gap-1">
      <ReactECharts option={option} notMerge style={{ height: hasMemory ? 310 : 220, width: '100%' }} />
      {collapsed.length > 0 && (
        <p className="text-xs/5 text-gray-500 dark:text-gray-400">
          Not drawn as lines:{' '}
          {collapsed.map((c, i) => (
            <span key={c.kind}>
              {i > 0 && ', '}
              {c.count} <span className="font-mono">{c.kind}</span> events
            </span>
          ))}
        </p>
      )}
    </div>
  )
}
