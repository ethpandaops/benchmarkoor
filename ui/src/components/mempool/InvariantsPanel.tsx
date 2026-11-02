import { useState } from 'react'
import clsx from 'clsx'
import type { MempoolScenarioResult } from '@/api/types'
import { Card } from '@/components/shared/Card'
import { INVARIANT_PREFIX } from './behaviours'
import { explainText } from './mempoolUtils'
import { openScenario, scenarioDomId, summarizeInvariants, type InvariantSummary } from './runOverview'
import { CheckIcon } from './CheckIcon'
import { ExplainButton, Explanation } from './Explain'

function InvariantRow({ inv }: { inv: InvariantSummary }) {
  const [open, setOpen] = useState(false)
  const failed = inv.failed > 0
  const name = inv.name.slice(INVARIANT_PREFIX.length)
  return (
    <li className="flex gap-2.5 py-1.5">
      <CheckIcon state={failed ? (inv.severity === 'hard' ? 'hard-fail' : 'soft-fail') : 'passed'} className="mt-0.5" />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex flex-wrap items-baseline gap-x-3">
          <span
            title={explainText(inv.explain)}
            className={clsx('min-w-0 flex-1 text-sm/5 break-words', failed ? 'font-medium text-gray-900 dark:text-gray-100' : 'text-gray-700 dark:text-gray-300')}
          >
            {name}
          </span>
          {inv.explain && <ExplainButton open={open} onClick={() => setOpen(!open)} label={false} />}
          <span className="w-36 shrink-0 text-xs/5 text-gray-500 dark:text-gray-400" title={inv.scenarios.join(', ')}>
            checked in {inv.scenarios.length} scenario{inv.scenarios.length === 1 ? '' : 's'}
          </span>
          <span className="w-28 shrink-0 text-right text-xs/5 tabular-nums">
            <span className="text-green-700 dark:text-green-400">{inv.passed} passed</span>
            {failed && <span className="font-medium text-red-700 dark:text-red-400">, {inv.failed} failed</span>}
          </span>
        </div>
        {failed && (
          <ul className="flex flex-col text-xs/5">
            {inv.failures.map((f, i) => (
              <li key={`${f.scenario}-${i}`} className="flex flex-wrap items-baseline gap-x-2">
                <a
                  href={`#${scenarioDomId(f.scenario)}`}
                  onClick={(e) => {
                    e.preventDefault()
                    openScenario(f.scenario)
                  }}
                  className="font-mono text-blue-600 hover:underline dark:text-blue-400"
                >
                  {f.scenario}
                </a>
                {f.actual && <span className="font-mono break-words text-gray-600 dark:text-gray-400">actual {f.actual}</span>}
              </li>
            ))}
          </ul>
        )}
        {open && inv.explain && <Explanation explain={inv.explain} />}
      </div>
    </li>
  )
}

// InvariantsPanel sums up the invariants: rules the harness checks after
// every build and import, so each repeats in many scenarios.
export function InvariantsPanel({ scenarios }: { scenarios: MempoolScenarioResult[] }) {
  const invariants = summarizeInvariants(scenarios)
  if (invariants.length === 0) return null
  const failed = invariants.filter((i) => i.failed > 0).length
  return (
    <Card
      title="Invariants"
      collapsible
      defaultCollapsed={failed === 0}
      headerExtra={
        <span className={clsx('text-xs/5 tabular-nums', failed > 0 ? 'font-medium text-red-700 dark:text-red-400' : 'text-gray-500 dark:text-gray-400')}>
          {failed > 0 ? `${failed} of ${invariants.length} failed` : `${invariants.length} of ${invariants.length} held`}
        </span>
      }
    >
      <div className="flex flex-col gap-2">
        <p className="text-xs/5 text-gray-500 dark:text-gray-400">
          The harness checks these rules after every payload build and block import. The scenario cards hide the passed invariant checks.
        </p>
        <ul className="divide-y divide-gray-100 dark:divide-gray-700/50">
          {invariants.map((inv) => (
            <InvariantRow key={inv.name} inv={inv} />
          ))}
        </ul>
      </div>
    </Card>
  )
}
