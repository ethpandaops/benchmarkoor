import { useEffect, useMemo, useState } from 'react'
import ReactECharts from 'echarts-for-react'
import type { IndexerPass } from '@/api/hooks/useAdmin'
import { formatPassDuration, passTriggerLabel } from '@/components/admin/constants'
import { formatDurationMs, formatNumber } from '@/utils/format'

/**
 * Series colours. Both modes are chosen for their own surface rather than
 * flipped, and the stack order below (re-indexed, new, failed) is the order
 * these three were validated in: it keeps the red away from the green, which
 * is the pair a red-green colour blindness cannot separate.
 */
const COLORS = {
  light: {
    reindexed: '#1baf7a',
    indexed: '#2a78d6',
    failed: '#e34948',
    duration: '#2a78d6',
  },
  dark: {
    reindexed: '#199e70',
    indexed: '#3987e5',
    failed: '#e66767',
    duration: '#3987e5',
  },
}

/** Reads the theme the way the other charts in the app do. */
function useDarkMode() {
  const [isDark, setIsDark] = useState(() =>
    document.documentElement.classList.contains('dark'),
  )

  useEffect(() => {
    const observer = new MutationObserver(() => {
      setIsDark(document.documentElement.classList.contains('dark'))
    })
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ['class'],
    })
    return () => observer.disconnect()
  }, [])

  return isDark
}

/** The clock time of a pass, which is all an axis label has room for. */
function passClock(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return '?'
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

/**
 * A duration for an axis tick. The shared formatter renders zero as "0ns",
 * which on an axis whose other ticks are seconds reads as a unit change
 * rather than as the origin.
 */
function axisDuration(ms: number): string {
  return ms === 0 ? '0' : formatDurationMs(ms)
}

function passDate(iso: string): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return 'unknown'
  return date.toLocaleString()
}

/** One tooltip body, shared by both charts so they read the same. */
function tooltipHTML(pass: IndexerPass, subTextColor: string): string {
  const lines = [
    `Duration: ${formatPassDuration(pass.duration_ms)}`,
    `New: ${formatNumber(pass.runs_indexed)}`,
    `Re-indexed: ${formatNumber(pass.runs_reindexed)}`,
    `Failed: ${formatNumber(pass.runs_failed)}`,
    `Skipped: ${formatNumber(pass.skipped_failures)}`,
    `Storage: ${formatNumber(pass.storage_runs)} runs, ` +
      `${formatNumber(pass.indexed_runs)} already indexed`,
  ]

  if (pass.status !== 'completed') {
    lines.push(`Status: ${pass.status}`)
  }

  if (pass.error) {
    lines.push(`Error: ${pass.error}`)
  }

  const trigger = passTriggerLabel(pass.trigger)

  return (
    `<strong>${passDate(pass.started_at)}</strong>` +
    `<span style="color:${subTextColor};"> · ${trigger}</span><br/>` +
    lines.join('<br/>')
  )
}

interface IndexerPassChartsProps {
  /** Passes newest first, the way the API returns them. */
  passes: IndexerPass[]
}

/**
 * Duration and throughput of the recent indexing passes, as two charts rather
 * than one with two y-axes: seconds and runs share no scale, and laying them
 * on top of each other invents a relationship between them.
 */
export function IndexerPassCharts({ passes }: IndexerPassChartsProps) {
  const isDark = useDarkMode()
  const colors = isDark ? COLORS.dark : COLORS.light

  const textColor = isDark ? '#e5e7eb' : '#374151'
  const subTextColor = isDark ? '#9ca3af' : '#6b7280'
  const gridColor = isDark ? '#374151' : '#e5e7eb'
  const surface = isDark ? '#111827' : '#ffffff'

  // Oldest first, so time runs left to right.
  const ordered = useMemo(() => [...passes].reverse(), [passes])
  const labels = useMemo(
    () => ordered.map((pass) => passClock(pass.started_at)),
    [ordered],
  )

  // One label every so often, or a hundred passes overprint each other.
  const labelInterval = Math.max(0, Math.ceil(ordered.length / 12) - 1)

  const sharedAxis = useMemo(
    () => ({
      type: 'category' as const,
      data: labels,
      axisLabel: {
        color: subTextColor,
        fontSize: 10,
        interval: labelInterval,
      },
      axisLine: { lineStyle: { color: gridColor } },
      axisTick: { show: false },
    }),
    [labels, labelInterval, subTextColor, gridColor],
  )

  const sharedTooltip = useMemo(
    () => ({
      trigger: 'axis' as const,
      axisPointer: { type: 'shadow' as const },
      backgroundColor: isDark ? '#1f2937' : '#ffffff',
      borderColor: gridColor,
      textStyle: { color: textColor, fontSize: 11 },
      extraCssText: 'max-width: 320px; white-space: normal;',
      formatter: (params: { dataIndex: number }[]) => {
        const pass = ordered[params[0]?.dataIndex ?? 0]
        return pass ? tooltipHTML(pass, subTextColor) : ''
      },
    }),
    [ordered, isDark, gridColor, textColor, subTextColor],
  )

  const durationOption = useMemo(
    () => ({
      tooltip: sharedTooltip,
      grid: { left: 64, right: 12, top: 12, bottom: 24 },
      xAxis: sharedAxis,
      yAxis: {
        type: 'value' as const,
        axisLabel: {
          color: subTextColor,
          fontSize: 10,
          formatter: axisDuration,
        },
        axisLine: { show: false },
        splitLine: { lineStyle: { color: gridColor, type: 'dashed' as const } },
      },
      series: [
        {
          type: 'bar' as const,
          data: ordered.map((pass) => pass.duration_ms),
          barMaxWidth: 14,
          itemStyle: {
            color: colors.duration,
            borderRadius: [4, 4, 0, 0] as [number, number, number, number],
          },
        },
      ],
    }),
    [ordered, sharedAxis, sharedTooltip, subTextColor, gridColor, colors],
  )

  const runsOption = useMemo(() => {
    // A hairline in the surface colour separates the stacked segments, so
    // where one ends and the next begins does not rest on colour alone.
    const stacked = (
      name: string,
      color: string,
      values: number[],
      top: boolean,
    ) => ({
      name,
      type: 'bar' as const,
      stack: 'runs',
      data: values,
      barMaxWidth: 14,
      itemStyle: {
        color,
        borderColor: surface,
        borderWidth: 1,
        borderRadius: top
          ? ([4, 4, 0, 0] as [number, number, number, number])
          : 0,
      },
    })

    return {
      tooltip: sharedTooltip,
      legend: {
        bottom: 0,
        itemWidth: 10,
        itemHeight: 10,
        textStyle: { color: textColor, fontSize: 11 },
      },
      grid: { left: 48, right: 12, top: 12, bottom: 44 },
      xAxis: sharedAxis,
      yAxis: {
        type: 'value' as const,
        minInterval: 1,
        axisLabel: {
          color: subTextColor,
          fontSize: 10,
          formatter: (value: number) => formatNumber(value),
        },
        axisLine: { show: false },
        splitLine: { lineStyle: { color: gridColor, type: 'dashed' as const } },
      },
      series: [
        stacked(
          'Re-indexed',
          colors.reindexed,
          ordered.map((pass) => pass.runs_reindexed),
          false,
        ),
        stacked(
          'New',
          colors.indexed,
          ordered.map((pass) => pass.runs_indexed),
          false,
        ),
        stacked(
          'Failed',
          colors.failed,
          ordered.map((pass) => pass.runs_failed),
          true,
        ),
      ],
    }
  }, [
    ordered,
    sharedAxis,
    sharedTooltip,
    textColor,
    subTextColor,
    gridColor,
    surface,
    colors,
  ])

  if (ordered.length === 0) {
    return (
      <div className="rounded-sm border border-gray-200 px-4 py-6 text-center text-sm text-gray-500 dark:border-gray-700 dark:text-gray-400">
        No pass recorded yet. The indexer writes one when a pass ends.
      </div>
    )
  }

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <figure className="m-0 rounded-sm border border-gray-200 px-3 py-3 dark:border-gray-700">
        <figcaption className="mb-1 text-xs font-medium text-gray-700 dark:text-gray-300">
          Pass duration
        </figcaption>
        <ReactECharts
          option={durationOption}
          style={{ height: '200px', width: '100%' }}
          opts={{ renderer: 'svg' }}
          notMerge
        />
      </figure>

      <figure className="m-0 rounded-sm border border-gray-200 px-3 py-3 dark:border-gray-700">
        <figcaption className="mb-1 text-xs font-medium text-gray-700 dark:text-gray-300">
          Runs per pass
        </figcaption>
        <ReactECharts
          option={runsOption}
          style={{ height: '200px', width: '100%' }}
          opts={{ renderer: 'svg' }}
          notMerge
        />
      </figure>
    </div>
  )
}
