import { Fragment, useMemo } from 'react'
import { Link } from '@tanstack/react-router'
import clsx from 'clsx'
import { useIndex } from '@/api/hooks/useIndex'
import { useMempoolResults } from '@/api/hooks/useMempoolResult'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { Card } from '@/components/shared/Card'
import {
  buildComparisonRows,
  computeDifferences,
  formatMempoolValue,
  selectMempoolPeerRuns,
  type ClientMempoolResult,
} from './mempoolUtils'
import { ScenarioStatusBadge } from './ScenarioCard'

interface MempoolComparisonProps {
  runId: string
  client: string
  suiteHash?: string
}

interface PeerRun {
  client: string
  runId: string
}

function ClientHeader({ peer, current }: { peer: PeerRun; current: boolean }) {
  return (
    <div className="flex flex-col items-end gap-0.5">
      <ClientBadge client={peer.client} />
      {current ? (
        <span className="text-xs/5 font-normal text-gray-500 dark:text-gray-400">this run</span>
      ) : (
        <Link
          to="/runs/$runId"
          params={{ runId: peer.runId }}
          className="max-w-28 truncate text-xs/5 font-normal text-blue-600 hover:text-blue-700 dark:text-blue-400 dark:hover:text-blue-300"
          title={peer.runId}
        >
          {peer.runId}
        </Link>
      )}
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
    if (!selected.some((p) => p.runId === runId)) {
      return [...selected.filter((p) => p.client !== client), { client, runId }].sort((a, b) =>
        a.client.localeCompare(b.client),
      )
    }
    return selected
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

  const rows = useMemo(() => buildComparisonRows(results), [results])
  const differences = useMemo(() => computeDifferences(results), [results])
  const clients = results.map((r) => r.client)

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

  const scenarioNames = [...new Set(rows.map((r) => r.scenario))]

  return (
    <Card title={`Client comparison (${clients.length} clients)`} collapsible>
      <div className="flex flex-col gap-6">
        <p className="text-xs/5 text-gray-500 dark:text-gray-400">
          Each column is the latest mempool run of a client on this suite. Green marks the best value and red the worst, for
          metrics that have a direction.
        </p>
        <div className="overflow-x-auto">
          <table className="min-w-full text-sm/6">
            <thead>
              <tr className="border-b border-gray-200 text-left text-xs/5 font-medium text-gray-500 dark:border-gray-700 dark:text-gray-400">
                <th className="py-2 pr-4">Metric</th>
                {results.map((r) => (
                  <th key={r.client} className="px-2 py-2 text-right">
                    <ClientHeader peer={r} current={r.runId === runId} />
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {scenarioNames.map((scenario) => (
                <Fragment key={scenario}>
                  <tr className="bg-gray-50 dark:bg-gray-900/40">
                    <td className="py-1.5 pr-4 font-mono text-xs/5 font-medium text-gray-900 dark:text-gray-100">{scenario}</td>
                    {results.map((r) => (
                      <td key={r.client} className="px-2 py-1.5 text-right">
                        <ScenarioStatusBadge status={r.result.scenarios.find((s) => s.name === scenario)?.status} />
                      </td>
                    ))}
                  </tr>
                  {rows
                    .filter((row) => row.scenario === scenario)
                    .map((row) => (
                      <tr key={row.key} className="border-b border-gray-100 dark:border-gray-700/50">
                        <td className="py-1.5 pr-4 pl-3 text-gray-700 dark:text-gray-300">
                          {row.label}
                          {row.better && (
                            <span className="ml-1.5 text-xs/5 text-gray-400 dark:text-gray-500">
                              ({row.better === 'lower' ? '↓' : '↑'} better)
                            </span>
                          )}
                        </td>
                        {results.map((r) => {
                          const best = row.best.includes(r.client)
                          const worst = row.worst.includes(r.client)
                          return (
                            <td
                              key={r.client}
                              className={clsx(
                                'whitespace-nowrap px-2 py-1.5 text-right font-mono text-gray-900 dark:text-gray-100',
                                best && 'bg-green-50 font-semibold text-green-800 dark:bg-green-900/30 dark:text-green-300',
                                worst && 'bg-red-50 text-red-800 dark:bg-red-900/30 dark:text-red-300',
                              )}
                              title={best ? 'Best' : worst ? 'Worst' : undefined}
                            >
                              {formatMempoolValue(row.values[r.client], row.unit)}
                            </td>
                          )
                        })}
                      </tr>
                    ))}
                </Fragment>
              ))}
            </tbody>
          </table>
        </div>

        <div className="flex flex-col gap-3">
          <h4 className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">Differences</h4>
          {differences.length === 0 ? (
            <p className="text-sm/6 text-gray-500 dark:text-gray-400">
              All clients have the same assertion results and observations.
            </p>
          ) : (
            differences.map((d) => (
              <div key={d.scenario} className="flex flex-col gap-2 rounded-xs border border-amber-200 p-3 dark:border-amber-900/50">
                <span className="font-mono text-xs/5 font-medium text-gray-900 dark:text-gray-100">{d.scenario}</span>
                <div className="overflow-x-auto">
                  <table className="min-w-full text-sm/6">
                    <thead>
                      <tr className="text-left text-xs/5 font-medium text-gray-500 dark:text-gray-400">
                        <th className="py-1 pr-4">Item</th>
                        {clients.map((c) => (
                          <th key={c} className="px-2 py-1">{c}</th>
                        ))}
                      </tr>
                    </thead>
                    <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
                      {d.assertions.map((a) => (
                        <tr key={`a-${a.name}`}>
                          <td className="py-1 pr-4 font-mono text-xs/5 text-gray-700 dark:text-gray-300">
                            {a.name}
                            <span className="ml-1.5 font-sans text-gray-400 dark:text-gray-500">({a.severity})</span>
                          </td>
                          {clients.map((c) => {
                            const v = a.values[c]
                            return (
                              <td
                                key={c}
                                className={clsx(
                                  'px-2 py-1 text-xs/5 font-medium',
                                  v === true && 'text-green-700 dark:text-green-400',
                                  v === false && a.severity === 'hard' && 'text-red-700 dark:text-red-400',
                                  v === false && a.severity === 'soft' && 'text-amber-700 dark:text-amber-400',
                                  v === undefined && 'text-gray-400 dark:text-gray-500',
                                )}
                              >
                                {v === undefined ? '-' : v ? 'passed' : 'failed'}
                              </td>
                            )
                          })}
                        </tr>
                      ))}
                      {d.observations.map((o) => (
                        <tr key={`o-${o.key}`}>
                          <td className="py-1 pr-4 text-xs/5 text-gray-700 dark:text-gray-300" title={o.key}>{o.label}</td>
                          {clients.map((c) => (
                            <td key={c} className="break-all px-2 py-1 font-mono text-xs/5 text-amber-800 dark:text-amber-300">
                              {o.values[c] ?? '-'}
                            </td>
                          ))}
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </div>
            ))
          )}
        </div>
      </div>
    </Card>
  )
}
