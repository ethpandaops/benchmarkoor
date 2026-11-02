import { useMemo, useState } from 'react'
import clsx from 'clsx'
import type { MempoolLatency } from '@/api/types'
import { formatDuration, formatSig, histogramBuckets, humanizeGroup } from './mempoolUtils'

const SPARK_WIDTH = 120
const SPARK_HEIGHT = 28
const BAR_GAP = 2

// HistogramSparkline draws the latency buckets as small bars with no axes.
// Hovering a bar shows its range and count.
function HistogramSparkline({ latency }: { latency: MempoolLatency }) {
  const buckets = useMemo(() => histogramBuckets(latency), [latency])
  const [hover, setHover] = useState<number | null>(null)

  if (buckets.length === 0) return <span className="text-xs/5 text-gray-400 dark:text-gray-500">-</span>

  const max = Math.max(...buckets.map((b) => b.count), 1)
  const total = buckets.reduce((acc, b) => acc + b.count, 0)
  const slot = SPARK_WIDTH / buckets.length
  const barWidth = Math.max(1, Math.min(14, slot - BAR_GAP))
  const hovered = hover !== null ? buckets[hover] : undefined
  const range =
    hover === null
      ? ''
      : hover === 0
        ? `≤ ${formatDuration(buckets[0].le)}`
        : `${formatDuration(buckets[hover - 1].le)} – ${formatDuration(buckets[hover].le)}`

  return (
    <div className="relative inline-block" onMouseLeave={() => setHover(null)}>
      <svg width={SPARK_WIDTH} height={SPARK_HEIGHT} role="img" aria-label={`Histogram of ${latency.label}`} className="block">
        {buckets.map((b, i) => {
          const h = b.count > 0 ? Math.max(1.5, (b.count / max) * (SPARK_HEIGHT - 2)) : 0
          const x = i * slot + (slot - barWidth) / 2
          return (
            <g key={b.le} onMouseEnter={() => setHover(i)}>
              <rect x={i * slot} y={0} width={slot} height={SPARK_HEIGHT} fill="transparent" />
              <rect
                x={x}
                y={SPARK_HEIGHT - h}
                width={barWidth}
                height={h}
                rx={1}
                className={clsx(
                  hover === i ? 'fill-gray-600 dark:fill-gray-300' : 'fill-gray-400 dark:fill-gray-500',
                )}
              />
            </g>
          )
        })}
        <line x1={0} x2={SPARK_WIDTH} y1={SPARK_HEIGHT - 0.5} y2={SPARK_HEIGHT - 0.5} className="stroke-gray-200 dark:stroke-gray-700" />
      </svg>
      {hovered && (
        <div className="pointer-events-none absolute top-1/2 right-full z-10 mr-2 -translate-y-1/2 rounded-xs border border-gray-200 bg-white px-2 py-1 text-xs/5 whitespace-nowrap text-gray-700 shadow-sm dark:border-gray-700 dark:bg-gray-900 dark:text-gray-200">
          <span className="font-medium">{range}</span>
          <span className="ml-2 tabular-nums">
            {hovered.count.toLocaleString()} ({formatSig((hovered.count / Math.max(total, 1)) * 100, 2)}%)
          </span>
        </div>
      )}
    </div>
  )
}

const COLUMNS = [
  { key: 'min', label: 'min', tone: 'muted' },
  { key: 'mean', label: 'mean', tone: 'normal' },
  { key: 'p50', label: 'p50', tone: 'strong' },
  { key: 'p90', label: 'p90', tone: 'normal' },
  { key: 'p99', label: 'p99', tone: 'strong' },
  { key: 'max', label: 'max', tone: 'muted' },
] as const

const TONE_CLASS = {
  muted: 'text-gray-500 dark:text-gray-400',
  normal: 'text-gray-700 dark:text-gray-300',
  strong: 'font-semibold text-gray-900 dark:text-gray-100',
}

// groupHint returns the group as a secondary line when the label does not
// already name it and the table has more than one group.
function groupHint(l: MempoolLatency, multiGroup: boolean): string | undefined {
  if (!multiGroup || !l.group) return undefined
  const g = humanizeGroup(l.group)
  return l.label.toLowerCase().includes(g.toLowerCase()) ? undefined : g
}

export function LatencyTable({ latencies }: { latencies: MempoolLatency[] }) {
  const multiGroup = new Set(latencies.map((l) => l.group ?? '')).size > 1

  return (
    <div className="overflow-x-auto">
      <table className="min-w-full text-sm/6">
        <thead>
          <tr className="border-b border-gray-200 text-xs/5 font-medium text-gray-500 dark:border-gray-700 dark:text-gray-400">
            <th className="py-1.5 pr-4 text-left font-medium">Latency</th>
            <th className="px-3 py-1.5 text-right font-medium">Count</th>
            {COLUMNS.map((c) => (
              <th key={c.key} className={clsx('px-3 py-1.5 text-right', c.tone === 'strong' ? 'font-semibold text-gray-700 dark:text-gray-200' : 'font-medium')}>
                {c.label}
              </th>
            ))}
            <th className="py-1.5 pl-4 text-left font-medium">Distribution</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
          {latencies.map((l) => {
            const hint = groupHint(l, multiGroup)
            return (
              <tr key={l.key}>
                <td className="py-1.5 pr-4 text-gray-900 dark:text-gray-100">
                  <div className="flex flex-col">
                    <span className="break-words">{l.label}</span>
                    {hint && <span className="text-xs/5 text-gray-500 dark:text-gray-400">{hint}</span>}
                  </div>
                </td>
                <td className="px-3 py-1.5 text-right whitespace-nowrap text-gray-500 tabular-nums dark:text-gray-400">
                  {l.count.toLocaleString()}
                </td>
                {COLUMNS.map((c) => (
                  <td key={c.key} className={clsx('px-3 py-1.5 text-right whitespace-nowrap tabular-nums', TONE_CLASS[c.tone])}>
                    {l.count > 0 ? formatDuration(l[c.key]) : '-'}
                  </td>
                ))}
                <td className="py-1.5 pl-4 align-middle">
                  <HistogramSparkline latency={l} />
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
