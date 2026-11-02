import { useMemo } from 'react'
import ReactECharts from 'echarts-for-react'
import type { MempoolLatency } from '@/api/types'
import { formatMempoolValue, histogramBuckets } from './mempoolUtils'
import { chartTheme, SERIES_COLORS, useIsDark } from './chartTheme'

function LatencyHistogram({ latency }: { latency: MempoolLatency }) {
  const isDark = useIsDark()
  const buckets = useMemo(() => histogramBuckets(latency), [latency])

  const option = useMemo(() => {
    const t = chartTheme(isDark)
    return {
      backgroundColor: 'transparent',
      animation: false,
      grid: { left: 36, right: 8, top: 8, bottom: 22 },
      tooltip: {
        trigger: 'item',
        backgroundColor: t.tooltipBg,
        borderColor: t.tooltipBorder,
        textStyle: { color: t.text, fontSize: 12 },
        formatter: (p: { name: string; value: number }) => `≤ ${p.name} ms: ${p.value.toLocaleString()}`,
      },
      xAxis: {
        type: 'category',
        data: buckets.map((b) => String(b.le)),
        axisLabel: { color: t.muted, fontSize: 10 },
        axisLine: { lineStyle: { color: t.axisLine } },
        axisTick: { show: false },
      },
      yAxis: {
        type: 'value',
        splitNumber: 2,
        axisLabel: { color: t.muted, fontSize: 10 },
        splitLine: { lineStyle: { color: t.splitLine } },
      },
      series: [
        {
          type: 'bar',
          barCategoryGap: '20%',
          itemStyle: { color: SERIES_COLORS.histogram, borderRadius: [4, 4, 0, 0] },
          data: buckets.map((b) => b.count),
        },
      ],
    }
  }, [isDark, buckets])

  if (buckets.length === 0) return <span className="text-xs/5 text-gray-400 dark:text-gray-500">-</span>

  return <ReactECharts option={option} notMerge style={{ height: 90, width: 220 }} />
}

const STAT_COLUMNS = ['min', 'mean', 'p50', 'p90', 'p99', 'max'] as const

export function LatencyTable({ latencies }: { latencies: MempoolLatency[] }) {
  return (
    <div className="overflow-x-auto">
      <table className="min-w-full divide-y divide-gray-200 text-sm/6 dark:divide-gray-700">
        <thead>
          <tr className="text-left text-xs/5 font-medium text-gray-500 dark:text-gray-400">
            <th className="py-2 pr-4">Latency</th>
            <th className="px-2 py-2 text-right">Count</th>
            {STAT_COLUMNS.map((c) => (
              <th key={c} className="px-2 py-2 text-right">{c}</th>
            ))}
            <th className="py-2 pl-4">Histogram (bucket ≤ ms)</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
          {latencies.map((l) => (
            <tr key={l.key} className="text-gray-900 dark:text-gray-100">
              <td className="py-2 pr-4">
                <div className="flex flex-col">
                  <span>{l.label}</span>
                  {l.group && <span className="text-xs/5 text-gray-500 dark:text-gray-400">{l.group}</span>}
                </div>
              </td>
              <td className="whitespace-nowrap px-2 py-2 text-right font-mono">{l.count.toLocaleString()}</td>
              {STAT_COLUMNS.map((c) => (
                <td key={c} className="whitespace-nowrap px-2 py-2 text-right font-mono">
                  {l.count > 0 ? formatMempoolValue(l[c], l.unit) : '-'}
                </td>
              ))}
              <td className="py-1 pl-4">
                <LatencyHistogram latency={l} />
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
