import { useMemo, useRef, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import type { MempoolEvent, MempoolTimelinePoint } from '@/api/types'
import { formatBytes } from '@/utils/format'
import { formatSig, groupEventsByKind } from './mempoolUtils'
import { chartTheme, eventKindColor, SERIES_COLORS, useIsDark } from './chartTheme'

interface PoolTimelineChartProps {
  timeline: MempoolTimelinePoint[]
  events: MempoolEvent[]
}

function formatSeconds(ms: number): string {
  return `${formatSig(ms / 1000)}s`
}

function escapeHtml(text: string): string {
  return text.replace(/[&<>"']/g, (c) => `&#${c.charCodeAt(0)};`)
}

// GRID_LEFT matches pl-19 (76px) on the legend row.
const GRID_LEFT = 76
const GRID_RIGHT = 16
const LANE_ROW = 14
const POOL_HEIGHT = 170
const MEMORY_HEIGHT = 56
const SLIDER_HEIGHT = 22
// Events nearer than this share of the time range to the crosshair go in
// the tooltip.
const EVENT_SNAP = 0.012
const MAX_TOOLTIP_EVENTS = 6

function Swatch({ color, label }: { color: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className="h-0.5 w-3 rounded-full" style={{ backgroundColor: color }} />
      {label}
    </span>
  )
}

// PoolTimelineChart draws the pending and queued pool sizes over the
// scenario time. Events sit as thin ticks in a lane per kind above the
// plot. Client memory, when present, is a separate small plot below with its
// own scale (no dual axis). One crosshair runs through all three.
export function PoolTimelineChart({ timeline, events }: PoolTimelineChartProps) {
  const isDark = useIsDark()
  const hasMemory = timeline.some((p) => p.memory_bytes !== undefined && p.memory_bytes !== null)
  const lanes = useMemo(() => groupEventsByKind(events), [events])

  const laneHeight = lanes.length * LANE_ROW
  const poolTop = lanes.length > 0 ? laneHeight + 18 : 12
  const memoryTitleTop = poolTop + POOL_HEIGHT + 18
  const memoryTop = memoryTitleTop + 22
  // The zoom slider sits under the last plot.
  const plotsBottom = hasMemory ? memoryTop + MEMORY_HEIGHT + 26 : poolTop + POOL_HEIGHT + 26
  const height = plotsBottom + SLIDER_HEIGHT + 8

  const chartRef = useRef<ReactECharts>(null)
  const [zoomed, setZoomed] = useState(false)
  const onEvents = useMemo(
    () => ({
      datazoom: () => {
        const opt = chartRef.current?.getEchartsInstance().getOption() as
          | { dataZoom?: Array<{ start?: number; end?: number }> }
          | undefined
        const z = opt?.dataZoom?.[0]
        setZoomed(z !== undefined && ((z.start ?? 0) > 0 || (z.end ?? 100) < 100))
      },
    }),
    [],
  )
  const resetZoom = () => {
    chartRef.current?.getEchartsInstance().dispatchAction({ type: 'dataZoom', start: 0, end: 100 })
  }

  const option = useMemo(() => {
    const t = chartTheme(isDark)
    const maxT = Math.max(timeline.length > 0 ? timeline[timeline.length - 1].t_ms : 0, ...(events ?? []).map((e) => e.t_ms))

    type Grid = { name: 'lane' | 'pool' | 'memory'; top: number; height: number }
    const grids: Grid[] = []
    if (lanes.length > 0) grids.push({ name: 'lane', top: 8, height: laneHeight })
    grids.push({ name: 'pool', top: poolTop, height: POOL_HEIGHT })
    if (hasMemory) grids.push({ name: 'memory', top: memoryTop, height: MEMORY_HEIGHT })
    const gridIndex = (name: Grid['name']) => grids.findIndex((g) => g.name === name)
    const last = grids.length - 1

    const xAxis = grids.map((_, i) => ({
      type: 'value' as const,
      gridIndex: i,
      min: 0,
      max: maxT,
      axisLabel: { show: i === last, color: t.muted, fontSize: 11, formatter: formatSeconds },
      axisLine: { show: i === last || grids[i].name !== 'lane', lineStyle: { color: t.axisLine } },
      axisTick: { show: i === last },
      splitLine: { show: false },
    }))

    const yAxis = grids.map((g, i) => {
      if (g.name === 'lane') {
        return {
          type: 'category' as const,
          gridIndex: i,
          inverse: true,
          data: lanes.map((l) => l.kind),
          axisLabel: { color: t.muted, fontSize: 10, margin: 6, interval: 0 },
          axisLine: { show: false },
          axisTick: { show: false },
          splitLine: { show: false },
          axisPointer: { show: false },
        }
      }
      return {
        type: 'value' as const,
        gridIndex: i,
        splitNumber: g.name === 'memory' ? 2 : 4,
        axisLabel: {
          color: t.muted,
          fontSize: 11,
          formatter: g.name === 'memory' ? (v: number) => formatBytes(v) : (v: number) => formatSig(v),
        },
        splitLine: { lineStyle: { color: t.splitLine, opacity: 0.6 } },
        axisPointer: { show: false },
      }
    })

    const series: object[] = [
      {
        name: 'Pending',
        type: 'line',
        xAxisIndex: gridIndex('pool'),
        yAxisIndex: gridIndex('pool'),
        showSymbol: false,
        lineStyle: { width: 2 },
        itemStyle: { color: SERIES_COLORS.pending },
        data: timeline.map((p) => [p.t_ms, p.pending]),
      },
      {
        name: 'Queued',
        type: 'line',
        xAxisIndex: gridIndex('pool'),
        yAxisIndex: gridIndex('pool'),
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
        xAxisIndex: gridIndex('memory'),
        yAxisIndex: gridIndex('memory'),
        showSymbol: false,
        lineStyle: { width: 2 },
        itemStyle: { color: SERIES_COLORS.memory },
        areaStyle: { opacity: 0.08, color: SERIES_COLORS.memory },
        data: timeline.filter((p) => p.memory_bytes != null).map((p) => [p.t_ms, p.memory_bytes]),
      })
    }
    lanes.forEach((lane, i) => {
      series.push({
        name: `event:${lane.kind}`,
        type: 'scatter',
        xAxisIndex: gridIndex('lane'),
        yAxisIndex: gridIndex('lane'),
        symbol: 'rect',
        symbolSize: [2, LANE_ROW - 3],
        silent: true,
        itemStyle: { color: eventKindColor(lane.kind, i, isDark), opacity: 0.85 },
        data: lane.events.map((e) => [e.t_ms, lane.kind]),
      })
    })

    const allEvents = lanes.flatMap((l, i) => l.events.map((e) => ({ e, color: eventKindColor(l.kind, i, isDark) })))
    const dot = (color: string) =>
      `<span style="display:inline-block;width:8px;height:2px;border-radius:1px;background-color:${color};margin-right:6px;vertical-align:middle;"></span>`

    return {
      backgroundColor: 'transparent',
      textStyle: { color: t.text },
      animation: false,
      title: hasMemory
        ? { text: 'Client memory', left: GRID_LEFT - 4, top: memoryTitleTop, textStyle: { color: t.muted, fontSize: 11, fontWeight: 500 } }
        : undefined,
      tooltip: {
        trigger: 'axis',
        backgroundColor: t.tooltipBg,
        borderColor: t.tooltipBorder,
        textStyle: { color: t.text, fontSize: 12 },
        axisPointer: { type: 'line', lineStyle: { color: t.muted, width: 1 } },
        formatter: (params: Array<{ seriesName: string; color: string; value: [number, number]; axisValue: number }>) => {
          const ps = Array.isArray(params) ? params : [params]
          if (ps.length === 0) return ''
          const at = Number(ps[0].axisValue ?? ps[0].value[0])
          let html = `<strong>${formatSeconds(at)}</strong><br/>`
          for (const p of ps) {
            if (p.seriesName.startsWith('event:')) continue
            const v = p.seriesName === 'Client memory' ? formatBytes(p.value[1]) : p.value[1].toLocaleString()
            html += `${dot(p.color)}${p.seriesName}: <strong>${v}</strong><br/>`
          }
          const near = allEvents
            .filter(({ e }) => Math.abs(e.t_ms - at) <= maxT * EVENT_SNAP)
            .sort((a, b) => Math.abs(a.e.t_ms - at) - Math.abs(b.e.t_ms - at))
          for (const { e, color } of near.slice(0, MAX_TOOLTIP_EVENTS)) {
            const detail = e.detail ? `: ${escapeHtml(e.detail.length > 60 ? `${e.detail.slice(0, 60)}…` : e.detail)}` : ''
            html += `${dot(color)}<span style="opacity:.8">${formatSeconds(e.t_ms)}</span> ${escapeHtml(e.kind)}${detail}<br/>`
          }
          if (near.length > MAX_TOOLTIP_EVENTS) html += `<span style="opacity:.7">+${near.length - MAX_TOOLTIP_EVENTS} more events</span>`
          return html
        },
      },
      axisPointer: { link: [{ xAxisIndex: 'all' }] },
      // Zoom on the time axis of all plots together: the slider under the
      // chart, or pinch / Ctrl+scroll in the plot (plain scroll still scrolls
      // the page), and drag to pan once zoomed. filterMode 'none' keeps the
      // lines whole at the zoom edges.
      dataZoom: [
        {
          type: 'inside',
          xAxisIndex: grids.map((_, i) => i),
          filterMode: 'none',
          zoomOnMouseWheel: 'ctrl',
          moveOnMouseWheel: false,
          moveOnMouseMove: true,
        },
        {
          type: 'slider',
          xAxisIndex: grids.map((_, i) => i),
          filterMode: 'none',
          left: GRID_LEFT,
          right: GRID_RIGHT,
          bottom: 6,
          height: SLIDER_HEIGHT,
          showDetail: false,
          borderColor: t.axisLine,
          backgroundColor: 'transparent',
          fillerColor: isDark ? 'rgba(59, 130, 246, 0.18)' : 'rgba(59, 130, 246, 0.10)',
          handleStyle: { color: isDark ? '#9ca3af' : '#6b7280', borderColor: t.axisLine },
          moveHandleSize: 0,
          dataBackground: {
            lineStyle: { color: SERIES_COLORS.pending, opacity: 0.5, width: 1 },
            areaStyle: { color: SERIES_COLORS.pending, opacity: 0.08 },
          },
          selectedDataBackground: {
            lineStyle: { color: SERIES_COLORS.pending, opacity: 0.8, width: 1 },
            areaStyle: { color: SERIES_COLORS.pending, opacity: 0.15 },
          },
          textStyle: { color: t.muted },
        },
      ],
      grid: grids.map((g) => ({ left: GRID_LEFT, right: GRID_RIGHT, top: g.top, height: g.height })),
      xAxis,
      yAxis,
      series,
    }
  }, [isDark, timeline, events, lanes, hasMemory, laneHeight, poolTop, memoryTop, memoryTitleTop])

  if (timeline.length === 0) {
    return <p className="text-sm/6 text-gray-500 dark:text-gray-400">No pool timeline recorded.</p>
  }

  return (
    <div className="flex flex-col gap-1">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 pl-19 text-xs/5 text-gray-600 dark:text-gray-300">
        <Swatch color={SERIES_COLORS.pending} label="Pending" />
        <Swatch color={SERIES_COLORS.queued} label="Queued" />
        {lanes.length > 0 && (
          <span className="text-gray-500 dark:text-gray-400">
            Events: {lanes.map((l) => `${l.kind} ${l.events.length.toLocaleString()}`).join(', ')}
          </span>
        )}
        <span className="ml-auto flex items-center gap-3 text-gray-500 dark:text-gray-400">
          <span>Zoom: drag the slider, or pinch / Ctrl+scroll</span>
          {zoomed && (
            <button
              type="button"
              onClick={resetZoom}
              className="rounded-xs border border-gray-300 px-2 py-0.5 text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-700"
            >
              Reset zoom
            </button>
          )}
        </span>
      </div>
      <ReactECharts ref={chartRef} option={option} notMerge onEvents={onEvents} style={{ height, width: '100%' }} />
    </div>
  )
}
