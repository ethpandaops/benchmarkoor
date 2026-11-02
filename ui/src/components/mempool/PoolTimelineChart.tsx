import { useMemo, useRef, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import type { MempoolBlock, MempoolEvent, MempoolTimelinePoint } from '@/api/types'
import { formatBytes } from '@/utils/format'
import { blockAt, eventHelp, feeUnit, formatSig, groupEventsByKind } from './mempoolUtils'
import { chartTheme, eventKindColor, SERIES_COLORS, useIsDark } from './chartTheme'

// TimelineMark is a vertical line with a label on the pool plot, e.g. the
// time a check was recorded.
export interface TimelineMark {
  t_ms: number
  label: string
}

interface PoolTimelineChartProps {
  timeline: MempoolTimelinePoint[]
  events: MempoolEvent[]
  blocks?: MempoolBlock[]
  marks?: TimelineMark[]
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
const SMALL_HEIGHT = 56
const TITLE_GAP = 18
const TITLE_HEIGHT = 22
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

type PlotName = 'lane' | 'pool' | 'baseFee' | 'blobFee' | 'memory' | 'cpu'

interface Plot {
  name: PlotName
  title?: string
  top: number
  height: number
  titleTop?: number
}

// layoutPlots stacks the plots from the top. Each small plot below the
// pool plot has a title above it.
function layoutPlots(defs: { name: PlotName; title?: string; height: number }[], gap: number): { plots: Plot[]; bottom: number } {
  const plots: Plot[] = []
  let y = 8
  for (const d of defs) {
    if (d.name === 'lane') {
      plots.push({ ...d, top: y })
      y += d.height + 10
    } else if (d.title) {
      const titleTop = y + TITLE_GAP
      plots.push({ ...d, titleTop, top: titleTop + TITLE_HEIGHT })
      y = titleTop + TITLE_HEIGHT + d.height
    } else {
      plots.push({ ...d, top: y + gap })
      y += gap + d.height
    }
  }
  return { plots, bottom: y + 26 }
}

// PoolTimelineChart draws the pending and queued pool sizes over the
// scenario time. Events sit as thin ticks in a lane per kind above the
// plot. The base fee and blob base fee (when the run has blocks), client
// memory and CPU are separate small plots below with their own scales (no
// dual axis). One crosshair and one zoom run through all.
export function PoolTimelineChart({ timeline, events, blocks, marks }: PoolTimelineChartProps) {
  const isDark = useIsDark()
  const hasMemory = timeline.some((p) => p.memory_bytes !== undefined && p.memory_bytes !== null)
  const hasCPU = timeline.some((p) => p.cpu_percent !== undefined && p.cpu_percent !== null)
  const sortedBlocks = useMemo(() => [...(blocks ?? [])].sort((a, b) => a.t_ms - b.t_ms), [blocks])
  const hasBaseFee = sortedBlocks.length > 0
  const hasBlobFee = sortedBlocks.some((b) => b.blob_base_fee > 1)
  const lanes = useMemo(() => groupEventsByKind(events), [events])
  const hasMarks = (marks ?? []).length > 0

  const { plots, bottom } = useMemo(
    () =>
      layoutPlots([
        ...(lanes.length > 0 ? [{ name: 'lane' as const, height: lanes.length * LANE_ROW }] : []),
        { name: 'pool' as const, height: POOL_HEIGHT },
        ...(hasBaseFee ? [{ name: 'baseFee' as const, title: 'Base fee (gwei)', height: SMALL_HEIGHT }] : []),
        ...(hasBlobFee ? [{ name: 'blobFee' as const, title: 'Blob base fee', height: SMALL_HEIGHT }] : []),
        ...(hasMemory ? [{ name: 'memory' as const, title: 'Client memory', height: SMALL_HEIGHT }] : []),
        ...(hasCPU ? [{ name: 'cpu' as const, title: 'Client CPU (100% = one core)', height: SMALL_HEIGHT }] : []),
      ], hasMarks ? 14 : 4),
    [lanes.length, hasBaseFee, hasBlobFee, hasMemory, hasCPU, hasMarks],
  )
  const height = bottom + SLIDER_HEIGHT + 8

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
    const maxT = Math.max(
      timeline.length > 0 ? timeline[timeline.length - 1].t_ms : 0,
      ...(events ?? []).map((e) => e.t_ms),
      ...sortedBlocks.map((b) => b.t_ms),
      ...(marks ?? []).map((m) => m.t_ms),
    )
    const blobUnit = feeUnit(Math.max(0, ...sortedBlocks.map((b) => b.blob_base_fee)))
    const gridIndex = (name: PlotName) => plots.findIndex((g) => g.name === name)
    const last = plots.length - 1

    const xAxis = plots.map((g, i) => ({
      type: 'value' as const,
      gridIndex: i,
      min: 0,
      max: maxT,
      axisLabel: { show: i === last, color: t.muted, fontSize: 11, formatter: formatSeconds },
      axisLine: { show: i === last || g.name !== 'lane', lineStyle: { color: t.axisLine } },
      axisTick: { show: i === last },
      splitLine: { show: false },
    }))

    const smallFormatter: Partial<Record<PlotName, (v: number) => string>> = {
      memory: (v) => formatBytes(v),
      cpu: (v) => `${formatSig(v)}%`,
      baseFee: (v) => formatSig(v),
      blobFee: (v) => `${formatSig(v)} ${blobUnit.unit}`,
    }

    const yAxis = plots.map((g, i) => {
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
        scale: g.name === 'baseFee' || g.name === 'blobFee',
        splitNumber: g.name === 'pool' ? 4 : 2,
        axisLabel: { color: t.muted, fontSize: 11, formatter: smallFormatter[g.name] ?? ((v: number) => formatSig(v)) },
        splitLine: { lineStyle: { color: t.splitLine, opacity: 0.6 } },
        axisPointer: { show: false },
      }
    })

    const markColor = isDark ? SERIES_COLORS.markDark : SERIES_COLORS.mark
    const markLine =
      marks && marks.length > 0
        ? {
            symbol: 'none',
            silent: true,
            animation: false,
            lineStyle: { color: markColor, width: 1, type: 'dashed' },
            // The label is the time only, flat above the plot; the legend
            // row names the check.
            label: { show: true, position: 'end', distance: 2, color: markColor, fontSize: 10, formatter: (p: { name: string }) => p.name },
            data: marks.map((m) => ({
              xAxis: m.t_ms,
              name: formatSeconds(m.t_ms),
            })),
          }
        : undefined

    const line = (name: string, plot: PlotName, color: string, data: (number | undefined)[][], extra: object = {}) => ({
      name,
      type: 'line',
      xAxisIndex: gridIndex(plot),
      yAxisIndex: gridIndex(plot),
      showSymbol: false,
      lineStyle: { width: 2 },
      itemStyle: { color },
      data,
      ...extra,
    })

    // A step line holds each block's fee until the next block; the last
    // fee holds to the end of the scenario.
    const stepData = (value: (b: MempoolBlock) => number) => {
      const pts = sortedBlocks.map((b) => [b.t_ms, value(b)])
      if (pts.length > 0 && pts[pts.length - 1][0] < maxT) pts.push([maxT, pts[pts.length - 1][1]])
      return pts
    }

    const series: object[] = [
      line('Pending', 'pool', SERIES_COLORS.pending, timeline.map((p) => [p.t_ms, p.pending]), markLine ? { markLine } : {}),
      line('Queued', 'pool', SERIES_COLORS.queued, timeline.map((p) => [p.t_ms, p.queued])),
    ]
    if (hasBaseFee) {
      series.push(line('Base fee', 'baseFee', SERIES_COLORS.baseFee, stepData((b) => b.base_fee / 1e9), { step: 'end' }))
    }
    if (hasBlobFee) {
      series.push(line('Blob base fee', 'blobFee', SERIES_COLORS.blobFee, stepData((b) => b.blob_base_fee / blobUnit.scale), { step: 'end' }))
    }
    if (hasMemory) {
      series.push(
        line('Client memory', 'memory', SERIES_COLORS.memory, timeline.filter((p) => p.memory_bytes != null).map((p) => [p.t_ms, p.memory_bytes]), {
          areaStyle: { opacity: 0.08, color: SERIES_COLORS.memory },
        }),
      )
    }
    if (hasCPU) {
      series.push(
        line('Client CPU', 'cpu', SERIES_COLORS.cpu, timeline.filter((p) => p.cpu_percent != null).map((p) => [p.t_ms, p.cpu_percent]), {
          areaStyle: { opacity: 0.08, color: SERIES_COLORS.cpu },
        }),
      )
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
    const valueText: Record<string, (v: number) => string> = {
      'Client memory': (v) => formatBytes(v),
      'Client CPU': (v) => `${formatSig(v)}%`,
      'Base fee': (v) => `${formatSig(v)} gwei`,
      'Blob base fee': (v) => `${formatSig(v)} ${blobUnit.unit}`,
    }

    return {
      backgroundColor: 'transparent',
      textStyle: { color: t.text },
      animation: false,
      title: plots
        .filter((g) => g.title)
        .map((g) => ({ text: g.title, top: g.titleTop, left: GRID_LEFT - 4, textStyle: { color: t.muted, fontSize: 11, fontWeight: 500 } })),
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
          let html = `<strong>${formatSeconds(at)}</strong>`
          const head = blockAt(sortedBlocks, at)
          if (head) {
            const gas = head.gas_limit > 0 ? `, ${formatSig((head.gas_used / head.gas_limit) * 100, 2)}% gas` : ''
            html += ` <span style="opacity:.8">· block #${head.number.toLocaleString()} (${head.txs.toLocaleString()} txs${gas})</span>`
          }
          html += '<br/>'
          // Step lines give one value per series; skip duplicates at a step.
          const seen = new Set<string>()
          for (const p of ps) {
            if (p.seriesName.startsWith('event:') || seen.has(p.seriesName)) continue
            seen.add(p.seriesName)
            const v = valueText[p.seriesName]?.(p.value[1]) ?? p.value[1].toLocaleString()
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
          xAxisIndex: plots.map((_, i) => i),
          filterMode: 'none',
          zoomOnMouseWheel: 'ctrl',
          moveOnMouseWheel: false,
          moveOnMouseMove: true,
        },
        {
          type: 'slider',
          xAxisIndex: plots.map((_, i) => i),
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
      grid: plots.map((g) => ({ left: GRID_LEFT, right: GRID_RIGHT, top: g.top, height: g.height })),
      xAxis,
      yAxis,
      series,
    }
  }, [isDark, timeline, events, lanes, plots, sortedBlocks, hasBaseFee, hasBlobFee, hasMemory, hasCPU, marks])

  if (timeline.length === 0) {
    return <p className="text-sm/6 text-gray-500 dark:text-gray-400">No pool timeline recorded.</p>
  }

  return (
    <div className="flex flex-col gap-1">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 pl-19 text-xs/5 text-gray-600 dark:text-gray-300">
        <Swatch color={SERIES_COLORS.pending} label="Pending" />
        <Swatch color={SERIES_COLORS.queued} label="Queued" />
        {hasBaseFee && <Swatch color={SERIES_COLORS.baseFee} label="Base fee" />}
        {hasBlobFee && <Swatch color={SERIES_COLORS.blobFee} label="Blob base fee" />}
        {(marks ?? []).map((m) => (
          <span key={`${m.t_ms}-${m.label}`} title={m.label} className="inline-flex max-w-md min-w-0 items-center gap-1.5 font-medium text-gray-900 dark:text-gray-100">
            <span className="h-3 shrink-0 border-l border-dashed border-gray-900 dark:border-gray-100" />
            <span className="shrink-0 tabular-nums">{formatSeconds(m.t_ms)}</span>
            <span className="truncate font-normal text-gray-600 dark:text-gray-300">{m.label}</span>
          </span>
        ))}
        {lanes.length > 0 && (
          <span className="flex flex-wrap items-center gap-x-3 gap-y-1 text-gray-500 dark:text-gray-400">
            <span>Events:</span>
            {lanes.map((l, i) => (
              <span
                key={l.kind}
                title={eventHelp(l.kind)}
                className="inline-flex cursor-help items-center gap-1 underline decoration-gray-300 decoration-dotted underline-offset-2 dark:decoration-gray-600"
              >
                <span className="h-2.5 w-0.5 rounded-xs" style={{ backgroundColor: eventKindColor(l.kind, i, isDark) }} />
                {l.kind} {l.events.length.toLocaleString()}
              </span>
            ))}
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
