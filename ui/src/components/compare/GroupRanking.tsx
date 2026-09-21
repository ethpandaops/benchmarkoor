import { useMemo } from 'react'
import clsx from 'clsx'
import { Medal, Trophy } from 'lucide-react'
import type { AggregatedStats } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { formatDuration, formatNumber } from '@/utils/format'
import { formatDurationSeconds } from '@/utils/date'
import { type CompareRun, type LabelMode, MEDAL_CLASSES, MEDAL_SIZES, RUN_SLOTS, formatRunLabel } from './constants'
import { computeMetrics, formatGas } from './compareMetrics'

interface GroupRankingProps {
  /** One synthetic run per group. */
  runs: CompareRun[]
  stepFilter: StepTypeOption[]
  labelMode: LabelMode
  testNameFilter?: (name: string) => boolean
  /** Position of the baseline in `runs`, the same convention as the other compare widgets. */
  baselineIdx: number
  onBaselineChange: (idx: number) => void
  /** Group index (`run.index`) whose won tests the heatmap highlights, or null. */
  highlightGroupIdx: number | null
  onHighlightChange: (groupIdx: number | null) => void
}

function calculateMGasPerSec(stats: AggregatedStats | undefined): number | undefined {
  if (!stats || stats.gas_used_time_total <= 0 || stats.gas_used_total <= 0) return undefined
  return (stats.gas_used_total * 1000) / stats.gas_used_time_total
}

/** Signed percentage of `value` against `base`, coloured by whether it is an improvement. */
function Delta({ value, base, higherIsBetter }: { value: number | undefined; base: number | undefined; higherIsBetter: boolean }) {
  if (value === undefined || base === undefined || base === 0) return null
  const pct = ((value - base) / base) * 100
  if (Math.abs(pct) < 0.05) return <span className="text-gray-400 dark:text-gray-500">±0%</span>
  const good = higherIsBetter ? pct > 0 : pct < 0
  return (
    <span className={good ? 'text-green-600 dark:text-green-400' : 'text-red-600 dark:text-red-400'}>
      {pct > 0 ? '+' : ''}{pct.toFixed(1)}%
    </span>
  )
}

/**
 * GroupRanking counts, for every test, which group had the highest
 * MGas/s, and ranks the groups by the number of tests they won. The
 * summary metrics of each group sit on the same row, with their change
 * against the baseline. A test counts only when at least two groups have
 * a value for it.
 */
export function GroupRanking({
  runs,
  stepFilter,
  labelMode,
  testNameFilter,
  baselineIdx,
  onBaselineChange,
  highlightGroupIdx,
  onHighlightChange,
}: GroupRankingProps) {
  const { ranking, testCount } = useMemo(() => {
    // MGas/s per test per group, in `runs` order.
    const byTest = new Map<string, (number | undefined)[]>()
    runs.forEach((run, gi) => {
      if (!run.result) return
      for (const [name, entry] of Object.entries(run.result.tests)) {
        if (testNameFilter && !testNameFilter(name)) continue
        let values = byTest.get(name)
        if (!values) {
          values = new Array<number | undefined>(runs.length).fill(undefined)
          byTest.set(name, values)
        }
        values[gi] = calculateMGasPerSec(getAggregatedStats(entry, stepFilter))
      }
    })

    const wins = new Array<number>(runs.length).fill(0)
    let testCount = 0
    for (const values of byTest.values()) {
      const known = values.filter((v): v is number => v !== undefined)
      if (known.length < 2) continue
      testCount++
      const best = Math.max(...known)
      // A tie is a win for every group on the best value.
      values.forEach((v, gi) => {
        if (v === best) wins[gi]++
      })
    }

    const ranking = runs
      .map((run, pos) => ({ run, pos, wins: wins[pos], metrics: computeMetrics(run.config, run.result, stepFilter, testNameFilter) }))
      .sort((a, b) => b.wins - a.wins || a.run.index - b.run.index)

    return { ranking, testCount }
  }, [runs, stepFilter, testNameFilter])

  if (runs.length < 2 || testCount === 0) return null

  const maxWins = ranking[0].wins
  const base = ranking.find((r) => r.pos === baselineIdx)?.metrics

  const th = 'px-2 py-1 text-right font-medium'

  return (
    <div className="flex flex-col gap-3 rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <Trophy className="size-4 text-gray-400 dark:text-gray-500" />
          <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Ranking</h3>
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">
            tests won by the highest MGas/s, over {testCount} tests · click a group to highlight its won tests on the heatmap
          </span>
        </div>
        <div className="flex items-center gap-1.5 text-xs/5 text-gray-500 dark:text-gray-400">
          <span>Baseline:</span>
          <div className="flex gap-1">
            {runs.map((run, i) => {
              const slot = RUN_SLOTS[run.index]
              return (
                <button
                  key={slot.label}
                  onClick={() => onBaselineChange(i)}
                  className={clsx(
                    'inline-flex items-center gap-1 rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors',
                    baselineIdx === i
                      ? `${slot.badgeBgClass} ${slot.badgeTextClass} ring-1 ring-current`
                      : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
                  )}
                >
                  <img src={`/img/clients/${run.config.instance.client}.jpg`} alt="" className="size-3.5 rounded-full object-cover" />
                  {formatRunLabel(slot, run, labelMode)}
                </button>
              )
            })}
          </div>
        </div>
      </div>

      <div className="overflow-x-auto">
        <table className="w-full text-xs/5">
          <thead>
            <tr className="border-b border-gray-200 text-[10px] uppercase tracking-wide text-gray-400 dark:border-gray-700 dark:text-gray-500">
              <th className="w-7 px-1 py-1" />
              <th className="px-2 py-1 text-left font-medium">Group</th>
              <th className="px-2 py-1 text-left font-medium" title="Tests where this group had the highest MGas/s">Wins</th>
              <th className={th} title="Total gas over total payload time of the tests">MGas/s</th>
              <th className={th} title="Sum of the test step durations">Test duration</th>
              <th className={th} title="Wall time of the newest sampled run, start to end">Runtime</th>
              <th className={th}>Tests</th>
              <th className={th}>Gas</th>
              <th className={th} title="Engine and RPC calls">Calls</th>
            </tr>
          </thead>
          <tbody className="text-gray-700 dark:text-gray-200">
            {ranking.map(({ run, pos, wins, metrics: m }, place) => {
              const slot = RUN_SLOTS[run.index]
              const share = (wins / testCount) * 100
              const leader = place === 0 && wins > 0
              const highlighted = highlightGroupIdx === run.index
              const isBaseline = pos === baselineIdx
              return (
                <tr
                  key={run.index}
                  onClick={() => onHighlightChange(highlighted ? null : run.index)}
                  title={highlighted ? 'Clear the highlight' : 'Highlight the tests this group wins on the heatmap'}
                  className={clsx(
                    'cursor-pointer border-b border-gray-100 last:border-0 dark:border-gray-700/50',
                    highlighted ? 'bg-blue-50 dark:bg-blue-900/30' : 'hover:bg-gray-50 dark:hover:bg-gray-700/50',
                  )}
                >
                  <td className={clsx('px-1 py-1 text-center font-mono', leader ? 'font-semibold text-gray-900 dark:text-gray-100' : 'text-gray-400 dark:text-gray-500')}>
                    <span className="flex h-5 items-center justify-center">
                      {place < MEDAL_CLASSES.length && wins > 0
                        ? <Medal className={clsx(MEDAL_SIZES[place], MEDAL_CLASSES[place])} aria-label={`${place + 1}. place`} />
                        : `${place + 1}.`}
                    </span>
                  </td>
                  <td className="px-2 py-1">
                    <span className={clsx('inline-flex max-w-56 items-center gap-1.5 truncate rounded-sm px-2 py-0.5 font-medium', slot.badgeBgClass, slot.badgeTextClass, highlighted && 'ring-1 ring-current')}>
                      <img src={`/img/clients/${run.config.instance.client}.jpg`} alt="" className="size-3.5 shrink-0 rounded-full object-cover" />
                      <span className="shrink-0">{run.config.instance.client}</span>
                      <span className="truncate opacity-70">{formatRunLabel(slot, run, labelMode)}</span>
                    </span>
                    {isBaseline && <span className="ml-1.5 text-[10px]/4 uppercase tracking-wide text-gray-400 dark:text-gray-500">baseline</span>}
                  </td>
                  <td className="px-2 py-1">
                    <span className="flex items-center gap-2">
                      <span className="relative h-3 w-24 shrink-0 overflow-hidden rounded-xs bg-gray-100 dark:bg-gray-700">
                        <span
                          className="block h-full rounded-xs"
                          style={{ width: `${maxWins > 0 ? (wins / maxWins) * 100 : 0}%`, backgroundColor: slot.color, opacity: leader ? 1 : 0.6 }}
                        />
                      </span>
                      <span className="font-mono tabular-nums">
                        {wins} <span className="text-gray-400 dark:text-gray-500">({share.toFixed(share < 10 ? 1 : 0)}%)</span>
                      </span>
                    </span>
                  </td>
                  <td className="px-2 py-1 text-right font-mono tabular-nums">
                    <span className="font-semibold text-gray-900 dark:text-gray-100">{m.mgasPerSec !== undefined ? m.mgasPerSec.toFixed(2) : '—'}</span>
                    {!isBaseline && <span className="ml-1.5"><Delta value={m.mgasPerSec} base={base?.mgasPerSec} higherIsBetter /></span>}
                  </td>
                  <td className="px-2 py-1 text-right font-mono tabular-nums">
                    {m.totalDuration > 0 ? formatDuration(m.totalDuration) : '—'}
                    {!isBaseline && m.totalDuration > 0 && <span className="ml-1.5"><Delta value={m.totalDuration} base={base?.totalDuration} higherIsBetter={false} /></span>}
                  </td>
                  <td className="px-2 py-1 text-right font-mono tabular-nums">
                    {m.totalRuntime !== undefined ? formatDurationSeconds(m.totalRuntime) : '—'}
                    {!isBaseline && <span className="ml-1.5"><Delta value={m.totalRuntime} base={base?.totalRuntime} higherIsBetter={false} /></span>}
                  </td>
                  <td className="px-2 py-1 text-right font-mono tabular-nums">
                    {m.testCount}
                    {m.failedTests > 0 && <span className="ml-1.5 text-red-600 dark:text-red-400">{m.failedTests} failed</span>}
                  </td>
                  <td className="px-2 py-1 text-right font-mono tabular-nums">{formatGas(m.totalGasUsed)}</td>
                  <td className="px-2 py-1 text-right font-mono tabular-nums">{formatNumber(m.totalMsgCount)}</td>
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>
    </div>
  )
}
