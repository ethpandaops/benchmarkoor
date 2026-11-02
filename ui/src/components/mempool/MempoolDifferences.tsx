import { useState } from 'react'
import clsx from 'clsx'
import { ChevronDown } from 'lucide-react'
import { ClientBadge } from '@/components/shared/ClientBadge'
import {
  checkState,
  type ClientMempoolResult,
  type ObservationDifference,
  type ObservationOutcome,
  type ScenarioDifferences,
  explainText,
} from './mempoolUtils'
import { CheckChip, SeverityTag } from './CheckIcon'

interface MempoolDifferencesProps {
  results: ClientMempoolResult[]
  differences: ScenarioDifferences[]
  currentRunId: string
}

const CURRENT_COL = 'bg-blue-50/60 dark:bg-blue-900/15'

const OUTCOME_CLASS: Record<Exclude<ObservationOutcome, 'value'>, string> = {
  accepted: 'bg-blue-50 text-blue-700 ring-1 ring-blue-200 dark:bg-blue-900/30 dark:text-blue-300 dark:ring-blue-800/60',
  rejected: 'bg-gray-100 text-gray-700 ring-1 ring-gray-200 dark:bg-gray-700 dark:text-gray-300 dark:ring-gray-600',
}

function ObservationCell({ value, outcome }: { value: string | undefined; outcome: ObservationOutcome | undefined }) {
  if (value === undefined || outcome === undefined) return <span className="text-gray-400 dark:text-gray-500">-</span>
  if (outcome === 'value') {
    return (
      <span className="line-clamp-3 font-mono break-words text-gray-900 dark:text-gray-100" title={value}>
        {value}
      </span>
    )
  }
  const showText = value.trim().toLowerCase() !== outcome
  return (
    <div className="flex flex-col gap-0.5">
      <span className={clsx('self-start rounded-xs px-1.5 text-xs/5 font-medium', OUTCOME_CLASS[outcome])}>{outcome}</span>
      {showText && (
        <span className="line-clamp-2 font-mono break-words text-gray-500 dark:text-gray-400" title={value}>
          {value}
        </span>
      )}
    </div>
  )
}

function ObservationRow({ o, results, currentRunId }: { o: ObservationDifference; results: ClientMempoolResult[]; currentRunId: string }) {
  return (
    <tr className="border-t border-gray-100 align-top dark:border-gray-700/50">
      <td className="px-3 py-1.5 break-words text-gray-700 dark:text-gray-300" title={explainText(o.explain, true) ?? o.key}>
        {o.label}
      </td>
      {results.map((r) => (
        <td key={r.client} className={clsx('px-3 py-1.5', r.runId === currentRunId && CURRENT_COL)}>
          <ObservationCell value={o.values[r.client]} outcome={o.outcomes[r.client]} />
        </td>
      ))}
    </tr>
  )
}

function ScenarioDifferenceTable({ d, results, currentRunId }: { d: ScenarioDifferences; results: ClientMempoolResult[]; currentRunId: string }) {
  const [showWording, setShowWording] = useState(false)
  const colSpan = results.length + 1

  return (
    <div className="flex flex-col gap-1.5">
      <span className="font-mono text-xs/5 font-medium text-gray-900 dark:text-gray-100">{d.scenario}</span>
      <div className="overflow-x-auto rounded-xs border border-gray-200 dark:border-gray-700">
        <table className="w-full min-w-3xl table-fixed text-xs/5">
          <colgroup>
            <col className="w-72" />
            {results.map((r) => (
              <col key={r.client} />
            ))}
          </colgroup>
          <thead>
            <tr className="bg-gray-50 text-left font-medium text-gray-500 dark:bg-gray-900/40 dark:text-gray-400">
              <th className="px-3 py-1.5 font-medium">Check / observation</th>
              {results.map((r) => (
                <th key={r.client} className={clsx('px-3 py-1.5', r.runId === currentRunId && CURRENT_COL)}>
                  <ClientBadge client={r.client} className="max-w-full" />
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {d.assertions.map((a) => (
              <tr key={`a-${a.name}`} className="border-t border-gray-100 align-top dark:border-gray-700/50">
                <td className="px-3 py-1.5 text-gray-700 dark:text-gray-300">
                  <span className="break-words" title={explainText(a.explain)}>
                    {a.name}
                  </span>{' '}
                  <SeverityTag severity={a.severity} />
                </td>
                {results.map((r) => {
                  const v = a.values[r.client]
                  return (
                    <td key={r.client} className={clsx('px-3 py-1.5', r.runId === currentRunId && CURRENT_COL)}>
                      {v === undefined ? (
                        <span className="text-gray-400 dark:text-gray-500">-</span>
                      ) : (
                        <CheckChip state={checkState({ severity: a.severity, passed: v })} />
                      )}
                    </td>
                  )
                })}
              </tr>
            ))}
            {d.observations.map((o) => (
              <ObservationRow key={`o-${o.key}`} o={o} results={results} currentRunId={currentRunId} />
            ))}
            {d.wordingOnly.length > 0 && (
              <tr className="border-t border-gray-100 dark:border-gray-700/50">
                <td colSpan={colSpan} className="px-1.5 py-1">
                  <button
                    type="button"
                    onClick={() => setShowWording(!showWording)}
                    aria-expanded={showWording}
                    className="inline-flex items-center gap-1 rounded-xs px-1.5 py-0.5 text-gray-500 hover:bg-gray-100 hover:text-gray-700 dark:text-gray-400 dark:hover:bg-gray-700/50 dark:hover:text-gray-200"
                  >
                    <ChevronDown className={clsx('size-3.5 transition-transform', !showWording && '-rotate-90')} />
                    Same outcome, different error text ({d.wordingOnly.length})
                  </button>
                </td>
              </tr>
            )}
            {showWording &&
              d.wordingOnly.map((o) => <ObservationRow key={`w-${o.key}`} o={o} results={results} currentRunId={currentRunId} />)}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// MempoolDifferences lists, per scenario, the checks and observations
// where the clients do not agree.
export function MempoolDifferences({ results, differences, currentRunId }: MempoolDifferencesProps) {
  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-col gap-0.5">
        <h4 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Differences</h4>
        <p className="text-xs/5 text-gray-500 dark:text-gray-400">
          Soft checks and observations that differ between clients are findings, not failures.
        </p>
      </div>
      {differences.length === 0 ? (
        <p className="text-sm/6 text-gray-500 dark:text-gray-400">All clients have the same check results and observations.</p>
      ) : (
        differences.map((d) => <ScenarioDifferenceTable key={d.scenario} d={d} results={results} currentRunId={currentRunId} />)
      )}
    </section>
  )
}
