import { useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import clsx from 'clsx'
import { ArrowDown, ArrowUp, ChevronDown } from 'lucide-react'
import { useIndex } from '@/api/hooks/useIndex'
import { useMempoolResults } from '@/api/hooks/useMempoolResult'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { Card } from '@/components/shared/Card'
import {
  barFraction,
  buildComparisonRows,
  computeDifferences,
  currentFirst,
  findScenario,
  relativeToBest,
  scenarioOrder,
  selectMempoolPeerRuns,
  valueFormatter,
  type ClientMempoolResult,
  type ComparisonRow,
} from './mempoolUtils'
import { ScenarioStatusBadge } from './ScenarioCard'
import { MempoolDifferences } from './MempoolDifferences'

interface MempoolComparisonProps {
  runId: string
  client: string
  suiteHash?: string
}

interface PeerRun {
  client: string
  runId: string
}

// Column classes for the current run: a subtle tint down the column.
const CURRENT_COL = 'bg-blue-50/60 dark:bg-blue-900/15'

function ClientHeader({ peer, current }: { peer: PeerRun; current: boolean }) {
  return (
    <div className="flex flex-col items-end gap-0.5">
      <ClientBadge client={peer.client} />
      {current ? (
        <span className="text-xs/5 font-medium text-blue-700 dark:text-blue-300">this run</span>
      ) : (
        <Link
          to="/runs/$runId"
          params={{ runId: peer.runId }}
          className="max-w-full truncate text-xs/5 font-normal text-gray-500 hover:text-blue-600 hover:underline dark:text-gray-400 dark:hover:text-blue-400"
          title={peer.runId}
        >
          {peer.runId}
        </Link>
      )}
    </div>
  )
}

function ValueCell({ row, client, current }: { row: ComparisonRow; client: string; current: boolean }) {
  const values = Object.values(row.values)
  const format = valueFormatter(row.unit, values)
  const value = row.values[client]
  const text = format(value)

  if (!row.better || value === undefined) {
    return (
      <td className={clsx('px-3 py-1.5 text-right whitespace-nowrap text-gray-700 tabular-nums dark:text-gray-300', current && CURRENT_COL)}>
        {text}
      </td>
    )
  }

  const best = row.best.length > 0 ? row.values[row.best[0]] : undefined
  const isBest = row.best.includes(client)
  const relative = isBest ? undefined : relativeToBest(value, best, row.better, row.unit, (v) => format(v))
  const fraction = barFraction(value, values)

  return (
    <td className={clsx('px-3 py-1', current && CURRENT_COL)} title={relative ? `${text}: ${relative} than the best (${format(best)})` : isBest ? `${text}: best` : text}>
      <div className="relative flex h-6 items-center justify-end gap-1.5">
        <div
          aria-hidden
          className="absolute inset-y-0.5 left-0 rounded-xs bg-blue-500/15 dark:bg-blue-400/20"
          style={{ width: `${Math.max(fraction * 100, value !== 0 ? 1 : 0)}%` }}
        />
        {isBest && (
          <span className="relative mr-auto pl-1 text-xs/4 font-medium text-blue-700 dark:text-blue-300">best</span>
        )}
        <span
          className={clsx(
            'relative whitespace-nowrap tabular-nums',
            isBest ? 'font-semibold text-gray-900 dark:text-gray-100' : 'text-gray-700 dark:text-gray-300',
          )}
        >
          {text}
        </span>
      </div>
      {relative && <div className="text-right text-xs/4 whitespace-nowrap text-gray-400 dark:text-gray-500">{relative}</div>}
    </td>
  )
}

interface ComparisonTableProps {
  results: ClientMempoolResult[]
  currentRunId: string
}

function ComparisonTable({ results, currentRunId }: ComparisonTableProps) {
  const rows = useMemo(() => buildComparisonRows(results), [results])
  const scenarios = useMemo(() => scenarioOrder(results), [results])
  const [showLatencies, setShowLatencies] = useState(false)
  // Open by default: the scenarios where the clients do not agree on the
  // status, or where one did not pass. The rest is one line each.
  const [expanded, setExpanded] = useState<Set<string>>(() => {
    const open = scenarios.filter((name) => {
      const statuses = results.map((r) => findScenario(r, name)?.status)
      return new Set(statuses).size > 1 || statuses.some((s) => s !== 'passed')
    })
    return new Set(open)
  })

  const visibleRows = (scenario: string) => rows.filter((r) => r.scenario === scenario && (showLatencies || r.kind === 'metric'))
  const allOpen = expanded.size === scenarios.length

  const toggle = (name: string) => {
    const next = new Set(expanded)
    if (next.has(name)) next.delete(name)
    else next.add(name)
    setExpanded(next)
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <label className="inline-flex cursor-pointer items-center gap-2 text-xs/5 text-gray-700 dark:text-gray-300">
          <input
            type="checkbox"
            checked={showLatencies}
            onChange={(e) => setShowLatencies(e.target.checked)}
            className="size-4 rounded-xs accent-blue-600"
          />
          Show latency percentiles (p50, p99)
        </label>
        <button
          type="button"
          onClick={() => setExpanded(allOpen ? new Set() : new Set(scenarios))}
          className="rounded-sm border border-gray-300 px-2.5 py-1 text-xs/5 font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700/50"
        >
          {allOpen ? 'Collapse all' : 'Expand all'}
        </button>
      </div>

      <div className="max-h-[80dvh] overflow-auto rounded-xs border border-gray-200 dark:border-gray-700">
        <table className="w-full min-w-3xl table-fixed text-sm/6">
          <colgroup>
            <col className="w-72" />
            {results.map((r) => (
              <col key={r.client} />
            ))}
          </colgroup>
          <thead className="sticky top-0 z-10 bg-white shadow-sm dark:bg-gray-800">
            <tr className="text-left text-xs/5 font-medium text-gray-500 dark:text-gray-400">
              <th className="px-3 py-2 align-bottom font-medium">Scenario / metric</th>
              {results.map((r) => (
                <th key={r.client} className={clsx('px-3 py-2 text-right', r.runId === currentRunId && CURRENT_COL)}>
                  <ClientHeader peer={r} current={r.runId === currentRunId} />
                </th>
              ))}
            </tr>
          </thead>
          {scenarios.map((scenario) => {
            const open = expanded.has(scenario)
            const scenarioRows = visibleRows(scenario)
            return (
              <tbody key={scenario} className="border-t border-gray-200 dark:border-gray-700">
                <tr
                  className="cursor-pointer bg-gray-50 hover:bg-gray-100 dark:bg-gray-900/40 dark:hover:bg-gray-900/70"
                  onClick={() => toggle(scenario)}
                  aria-expanded={open}
                >
                  <td className="px-3 py-1.5">
                    <span className="flex items-center gap-1.5">
                      <ChevronDown className={clsx('size-4 shrink-0 text-gray-400 transition-transform', !open && '-rotate-90')} />
                      <span className="truncate font-mono text-xs/5 font-medium text-gray-900 dark:text-gray-100">{scenario}</span>
                      <span className="shrink-0 text-xs/5 text-gray-400 dark:text-gray-500">{scenarioRows.length}</span>
                    </span>
                  </td>
                  {results.map((r) => (
                    <td key={r.client} className={clsx('px-3 py-1.5 text-right', r.runId === currentRunId && CURRENT_COL)}>
                      <ScenarioStatusBadge status={findScenario(r, scenario)?.status} />
                    </td>
                  ))}
                </tr>
                {open && scenarioRows.length === 0 && (
                  <tr>
                    <td colSpan={results.length + 1} className="px-3 py-1.5 pl-9 text-xs/5 text-gray-500 dark:text-gray-400">
                      {showLatencies ? 'No metrics or latencies.' : 'No metrics. Turn on the latency percentiles to see the latencies.'}
                    </td>
                  </tr>
                )}
                {open &&
                  scenarioRows.map((row) => (
                    <tr key={row.key} className="border-t border-gray-100 dark:border-gray-700/50">
                      <td className="px-3 py-1.5 pl-9 text-gray-700 dark:text-gray-300">
                        <span className="flex items-start gap-1">
                          <span className="break-words">{row.label}</span>
                          {row.better && (
                            <span title={`${row.better === 'lower' ? 'Lower' : 'Higher'} is better`} className="mt-1.5 shrink-0">
                              {row.better === 'lower' ? (
                                <ArrowDown className="size-3 text-gray-400 dark:text-gray-500" />
                              ) : (
                                <ArrowUp className="size-3 text-gray-400 dark:text-gray-500" />
                              )}
                            </span>
                          )}
                        </span>
                      </td>
                      {results.map((r) => (
                        <ValueCell key={r.client} row={row} client={r.client} current={r.runId === currentRunId} />
                      ))}
                    </tr>
                  ))}
              </tbody>
            )
          })}
        </table>
      </div>
    </div>
  )
}

// MempoolComparison compares this run with the latest mempool run of every
// other client on the same suite.
export function MempoolComparison({ runId, client, suiteHash }: MempoolComparisonProps) {
  const { data: index, isLoading: indexLoading } = useIndex()

  const peers = useMemo<PeerRun[]>(() => {
    const selected = selectMempoolPeerRuns(index?.entries ?? [], suiteHash, runId).map((e) => ({
      client: e.instance.client,
      runId: e.run_id,
    }))
    // The index can lag behind a new run. Always compare this run.
    const all = selected.some((p) => p.runId === runId) ? selected : [...selected.filter((p) => p.client !== client), { client, runId }]
    return currentFirst(
      [...all].sort((a, b) => a.client.localeCompare(b.client)),
      runId,
    )
  }, [index, suiteHash, runId, client])

  const { data: peerResults, isLoading: resultsLoading } = useMempoolResults(peers.map((p) => p.runId))
  const loading = indexLoading || resultsLoading

  const results = useMemo<ClientMempoolResult[]>(
    () =>
      peers.flatMap((p, i) => {
        const result = peerResults[i]
        return result ? [{ client: p.client, runId: p.runId, result }] : []
      }),
    [peers, peerResults],
  )

  const differences = useMemo(() => computeDifferences(results), [results])

  if (loading) {
    return (
      <Card title="Client comparison">
        <p className="text-sm/6 text-gray-500 dark:text-gray-400">Loading the results of the other clients...</p>
      </Card>
    )
  }

  if (results.length < 2) {
    return (
      <Card title="Client comparison">
        <p className="text-sm/6 text-gray-500 dark:text-gray-400">
          Only one client has run this suite. Run the same suite on other clients to compare them here.
        </p>
      </Card>
    )
  }

  return (
    <Card title={`Client comparison (${results.length} clients)`} collapsible>
      <div className="flex flex-col gap-8">
        <div className="flex flex-col gap-3">
          <p className="text-xs/5 text-gray-500 dark:text-gray-400">
            Each column is the latest mempool run of a client on this suite. For a metric with a direction, the bar shows
            the value against the largest in the row and the best value is in bold.
          </p>
          <ComparisonTable results={results} currentRunId={runId} />
        </div>
        <MempoolDifferences results={results} differences={differences} currentRunId={runId} />
      </div>
    </Card>
  )
}
