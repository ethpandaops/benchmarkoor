import { useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import clsx from 'clsx'
import { History } from 'lucide-react'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { Pagination } from '@/components/shared/Pagination'
import { SegmentedControl } from '@/components/shared/SegmentedControl'
import { TestName } from '@/components/shared/TestName'
import { formatDuration } from '@/utils/format'
import {
  MIN_DURATION_OPTIONS_MS,
  NOISE_FACTOR,
  REGRESSION_THRESHOLD_OPTIONS,
  significantChange,
  type ChangeDirection,
  type TestChange,
} from '@/utils/regressions'
import {
  BASELINE_RUNS,
  CHANGE_WINDOW_OPTIONS,
  DEFAULT_CHANGE_WINDOW,
  formatHistoryValue,
  HISTORY_METRIC_LABELS,
} from '@/utils/testHistory'

const PAGE_SIZE = 50

// Formats a whole number of milliseconds as a short label, such as 500ms or 1s.
function formatMs(ms: number): string {
  return ms >= 1000 ? `${ms / 1000}s` : `${ms}ms`
}

interface RegressionsPanelProps {
  changes: TestChange[]
  suiteHash: string
  // The steps search param of the suite page, passed on to the history page.
  steps?: string
  threshold: number
  onThresholdChange: (threshold: number) => void
  minDurationMs: number
  onMinDurationChange: (minDurationMs: number) => void
  direction: ChangeDirection
  onDirectionChange: (direction: ChangeDirection) => void
  // The number of recent runs per client that the step search looks at.
  stepWindow: number
  onStepWindowChange: (stepWindow: number) => void
}

// RegressionsPanel lists the tests where a client became much slower (or
// faster) in its recent runs, and since which run.
export function RegressionsPanel({
  changes,
  suiteHash,
  steps,
  threshold,
  onThresholdChange,
  minDurationMs,
  onMinDurationChange,
  direction,
  onDirectionChange,
  stepWindow,
  onStepWindowChange,
}: RegressionsPanelProps) {
  const [selectedClients, setSelectedClients] = useState<Set<string>>(new Set())
  const [page, setPage] = useState(1)

  const inDirection = useMemo(() => changes.filter((c) => c.direction === direction), [changes, direction])

  const countByClient = useMemo(() => {
    const counts = new Map<string, number>()
    for (const c of inDirection) counts.set(c.client, (counts.get(c.client) ?? 0) + 1)
    return [...counts.entries()].sort(([a], [b]) => a.localeCompare(b))
  }, [inDirection])

  const rows = useMemo(
    () => (selectedClients.size === 0 ? inDirection : inDirection.filter((c) => selectedClients.has(c.client))),
    [inDirection, selectedClients],
  )
  const totalPages = Math.max(1, Math.ceil(rows.length / PAGE_SIZE))
  const currentPage = Math.min(page, totalPages)
  const pagedRows = rows.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE)

  const toggleClient = (client: string) => {
    setPage(1)
    setSelectedClients((prev) => {
      const next = new Set(prev)
      if (next.has(client)) next.delete(client)
      else next.add(client)
      return next
    })
  }

  const label = direction === 'regression' ? 'regressions' : 'improvements'

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <div className="flex flex-col gap-3 border-b border-gray-200 px-4 py-3 dark:border-gray-700">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <SegmentedControl
            ariaLabel="Direction"
            value={direction}
            options={[
              { value: 'regression', label: 'Regressions' },
              { value: 'improvement', label: 'Improvements' },
            ]}
            onChange={(value) => {
              setPage(1)
              onDirectionChange(value)
            }}
          />
          <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
            <label className="flex items-center gap-2">
              <span className="text-xs/5 font-medium text-gray-700 sm:text-sm/6 dark:text-gray-300">Window:</span>
              <select
                value={stepWindow}
                onChange={(e) => {
                  setPage(1)
                  onStepWindowChange(Number(e.target.value))
                }}
                className="rounded-xs border border-gray-300 bg-white px-2 py-1 text-xs/5 text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
              >
                {CHANGE_WINDOW_OPTIONS.map((w) => (
                  <option key={w} value={w}>
                    Last {w} runs
                  </option>
                ))}
              </select>
            </label>
            <label className="flex items-center gap-2">
              <span className="text-xs/5 font-medium text-gray-700 sm:text-sm/6 dark:text-gray-300">Min. change:</span>
              <select
                value={threshold}
                onChange={(e) => {
                  setPage(1)
                  onThresholdChange(Number(e.target.value))
                }}
                className="rounded-xs border border-gray-300 bg-white px-2 py-1 text-xs/5 text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
              >
                {REGRESSION_THRESHOLD_OPTIONS.map((t) => (
                  <option key={t} value={t}>
                    {t}%
                  </option>
                ))}
              </select>
            </label>
            <label className="flex items-center gap-2">
              <span className="text-xs/5 font-medium text-gray-700 sm:text-sm/6 dark:text-gray-300">Min. block time:</span>
              <select
                value={minDurationMs}
                onChange={(e) => {
                  setPage(1)
                  onMinDurationChange(Number(e.target.value))
                }}
                className="rounded-xs border border-gray-300 bg-white px-2 py-1 text-xs/5 text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
              >
                {MIN_DURATION_OPTIONS_MS.map((ms) => (
                  <option key={ms} value={ms}>
                    {ms === 0 ? 'Any' : formatMs(ms)}
                  </option>
                ))}
              </select>
            </label>
          </div>
        </div>
        <p className="text-xs/5 text-gray-500 dark:text-gray-400">
          Each row finds the latest step in the last {stepWindow} runs of a client: the run where its level changed. It
          compares the median since that run with the median of up to {BASELINE_RUNS} runs before it. A change counts
          when it is at least {threshold}% and at least {NOISE_FACTOR}× the normal spread. A row stays until the level
          changes back or the step leaves the window.{' '}
          {minDurationMs > 0 && `The test must also take at least ${formatMs(minDurationMs)} before or since the step. `}
          Invalid-payload tests use the time to reject, also for the min. block time.
        </p>
        {countByClient.length > 0 && (
          <div className="flex flex-wrap items-center gap-2">
            {countByClient.map(([client, count]) => {
              const active = selectedClients.size === 0 || selectedClients.has(client)
              return (
                <button
                  key={client}
                  type="button"
                  onClick={() => toggleClient(client)}
                  aria-pressed={selectedClients.has(client)}
                  title={selectedClients.has(client) ? 'Show all clients' : 'Show only the selected clients'}
                  className={clsx(
                    'flex items-center gap-2 rounded-xs border px-2 py-1 transition-opacity',
                    selectedClients.has(client)
                      ? 'border-blue-500 dark:border-blue-400'
                      : 'border-gray-200 dark:border-gray-700',
                    !active && 'opacity-40',
                  )}
                >
                  <ClientBadge client={client} />
                  <span className="font-mono text-xs/5 font-medium text-gray-900 dark:text-gray-100">{count}</span>
                </button>
              )
            })}
          </div>
        )}
      </div>

      {rows.length === 0 ? (
        <p className="px-4 py-8 text-center text-sm/6 text-gray-500 dark:text-gray-400">
          No {label} of {threshold}% or more in the last {stepWindow} runs.
        </p>
      ) : (
        <div className="flex flex-col gap-4 pb-4">
          <div className="overflow-x-auto">
            <table className="min-w-full text-sm/6">
              <thead>
                <tr className="border-b border-gray-200 text-left text-xs/5 font-medium text-gray-500 uppercase dark:border-gray-700 dark:text-gray-400">
                  <th className="py-2 pr-2 pl-4">Client</th>
                  <th className="px-2 py-2 text-right">Change</th>
                  <th className="px-2 py-2">Test</th>
                  <th className="px-2 py-2 text-right">Now</th>
                  <th className="px-2 py-2 text-right">Before</th>
                  <th className="px-2 py-2">Since</th>
                  <th className="px-4 py-2" />
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
                {pagedRows.map((c) => (
                  <tr key={`${c.testName}\0${c.client}`} className="hover:bg-gray-50 dark:hover:bg-gray-700/50">
                    <td className="py-1.5 pr-2 pl-4">
                      <ClientBadge client={c.client} />
                    </td>
                    <td
                      className="px-2 py-1.5 text-right"
                      title={`Normal spread ±${c.baseline.noise.toFixed(1)}%, so the change must be at least ${significantChange(c.baseline, threshold).toFixed(1)}%`}
                    >
                      <span
                        className={clsx(
                          'rounded-xs px-1.5 py-0.5 font-mono text-xs/5 font-medium',
                          c.direction === 'regression'
                            ? 'bg-red-100 text-red-800 dark:bg-red-900/50 dark:text-red-300'
                            : 'bg-green-100 text-green-800 dark:bg-green-900/50 dark:text-green-300',
                        )}
                      >
                        {c.baseline.change > 0 ? '+' : ''}
                        {c.baseline.change.toFixed(1)}%
                      </span>
                    </td>
                    <td className="max-w-md min-w-64 px-2 py-1.5">
                      <TestName name={c.testName} />
                    </td>
                    <td
                      className="px-2 py-1.5 text-right font-mono whitespace-nowrap text-gray-900 dark:text-gray-100"
                      title={`Median ${HISTORY_METRIC_LABELS[c.metric]} of the ${c.step.runs} runs since the step`}
                    >
                      {formatHistoryValue(c.step.median, c.metric)}
                      {c.metric === 'mgas' && (
                        <div className="text-xs/5 text-gray-500 dark:text-gray-400">{formatDuration(c.step.duration_ns)}</div>
                      )}
                    </td>
                    <td
                      className="px-2 py-1.5 text-right font-mono whitespace-nowrap text-gray-500 dark:text-gray-400"
                      title={`Median of the ${c.baseline.runs} runs before the step`}
                    >
                      {formatHistoryValue(c.baseline.median, c.metric)}
                      {c.metric === 'mgas' && (
                        <div className="text-xs/5 text-gray-500 dark:text-gray-400">{formatDuration(c.baseline.duration_ns)}</div>
                      )}
                    </td>
                    <td className="px-2 py-1.5 whitespace-nowrap">
                      <Link
                        to="/runs/$runId"
                        params={{ runId: c.step.run_id }}
                        search={{ testModal: c.testName }}
                        className="text-xs/5 text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                        title="Open the first run since the step"
                      >
                        {new Date(c.step.run_start * 1000).toLocaleString()}
                      </Link>
                      <div className="text-xs/5 text-gray-500 dark:text-gray-400">
                        {c.step.runs === 1 ? 'latest run only' : `${c.step.runs} runs`}
                      </div>
                    </td>
                    <td className="px-4 py-1.5 text-right">
                      <Link
                        to="/suites/$suiteHash/history"
                        params={{ suiteHash }}
                        search={{
                          test: c.testName,
                          steps,
                          metric: c.metric === 'mgas' ? undefined : c.metric,
                          clients: c.client,
                          w: stepWindow === DEFAULT_CHANGE_WINDOW ? undefined : stepWindow,
                        }}
                        className="inline-flex items-center gap-1 text-xs/5 font-medium text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                        title={`Show the history of this test for ${c.client}`}
                      >
                        <History className="size-4" />
                        History
                      </Link>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <Pagination currentPage={currentPage} totalPages={totalPages} onPageChange={setPage} />
        </div>
      )}
    </div>
  )
}
