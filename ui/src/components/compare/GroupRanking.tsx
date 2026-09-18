import { useMemo } from 'react'
import clsx from 'clsx'
import { Trophy } from 'lucide-react'
import type { AggregatedStats } from '@/api/types'
import { type StepTypeOption, getAggregatedStats } from '@/pages/RunDetailPage'
import { type CompareRun, type LabelMode, RUN_SLOTS, formatRunLabel } from './constants'

interface GroupRankingProps {
  /** One synthetic run per group. */
  runs: CompareRun[]
  stepFilter: StepTypeOption[]
  labelMode: LabelMode
  testNameFilter?: (name: string) => boolean
}

function calculateMGasPerSec(stats: AggregatedStats | undefined): number | undefined {
  if (!stats || stats.gas_used_time_total <= 0 || stats.gas_used_total <= 0) return undefined
  return (stats.gas_used_total * 1000) / stats.gas_used_time_total
}

/**
 * GroupRanking counts, for every test, which group had the highest
 * MGas/s, and ranks the groups by the number of tests they won. A test
 * counts only when at least two groups have a value for it.
 */
export function GroupRanking({ runs, stepFilter, labelMode, testNameFilter }: GroupRankingProps) {
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
      .map((run, gi) => ({ run, wins: wins[gi] }))
      .sort((a, b) => b.wins - a.wins || a.run.index - b.run.index)

    return { ranking, testCount }
  }, [runs, stepFilter, testNameFilter])

  if (runs.length < 2 || testCount === 0) return null

  const maxWins = ranking[0].wins

  return (
    <div className="flex flex-col gap-3 rounded-sm bg-white p-4 shadow-xs dark:bg-gray-800">
      <div className="flex flex-wrap items-center gap-2">
        <Trophy className="size-4 text-gray-400 dark:text-gray-500" />
        <h3 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Ranking</h3>
        <span className="text-xs/5 text-gray-500 dark:text-gray-400">
          tests won by the highest MGas/s, over {testCount} tests
        </span>
      </div>

      <ol className="flex flex-col gap-1.5">
        {ranking.map(({ run, wins }, place) => {
          const slot = RUN_SLOTS[run.index]
          const share = testCount > 0 ? (wins / testCount) * 100 : 0
          const leader = place === 0 && wins > 0
          return (
            <li key={run.index} className="flex items-center gap-3 text-xs/5">
              <span className={clsx('w-5 shrink-0 text-right font-mono', leader ? 'font-semibold text-gray-900 dark:text-gray-100' : 'text-gray-400 dark:text-gray-500')}>
                {place + 1}.
              </span>
              <span className={clsx('inline-flex w-56 shrink-0 items-center gap-1.5 truncate rounded-sm px-2 py-0.5 font-medium', slot.badgeBgClass, slot.badgeTextClass)}>
                <img src={`/img/clients/${run.config.instance.client}.jpg`} alt="" className="size-3.5 shrink-0 rounded-full object-cover" />
                <span className="shrink-0">{run.config.instance.client}</span>
                <span className="truncate opacity-70">{formatRunLabel(slot, run, labelMode)}</span>
              </span>
              <div className="relative h-4 flex-1 overflow-hidden rounded-xs bg-gray-100 dark:bg-gray-700">
                <div
                  className="h-full rounded-xs transition-[width]"
                  style={{ width: `${maxWins > 0 ? (wins / maxWins) * 100 : 0}%`, backgroundColor: slot.color, opacity: leader ? 1 : 0.6 }}
                />
              </div>
              <span className="w-28 shrink-0 text-right font-mono tabular-nums text-gray-700 dark:text-gray-200">
                {wins} <span className="text-gray-400 dark:text-gray-500">({share.toFixed(share < 10 ? 1 : 0)}%)</span>
              </span>
            </li>
          )
        })}
      </ol>
    </div>
  )
}
