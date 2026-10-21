import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams, useSearch } from '@tanstack/react-router'
import clsx from 'clsx'
import { ExternalLink } from 'lucide-react'
import { useSuite } from '@/api/hooks/useSuite'
import { useTestHistory } from '@/api/hooks/useTestHistory'
import { ALL_INDEX_STEP_TYPES, DEFAULT_INDEX_STEP_FILTER, getRunDurationAggregatedStats, type IndexStepType } from '@/api/types'
import { Card } from '@/components/shared/Card'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { EmptyState } from '@/components/shared/EmptyState'
import { ErrorState } from '@/components/shared/ErrorState'
import { JDenticon } from '@/components/shared/JDenticon'
import { Pagination } from '@/components/shared/Pagination'
import { SegmentedControl } from '@/components/shared/SegmentedControl'
import { LoadingState } from '@/components/shared/Spinner'
import { TestName } from '@/components/shared/TestName'
import { TestHistoryChart, type HistoryXAxis } from '@/components/test-history/TestHistoryChart'
import { formatNumber } from '@/utils/format'
import { classifyChange, DEFAULT_REGRESSION_THRESHOLD, significantChange } from '@/utils/regressions'
import {
  BASELINE_RUNS,
  CHANGE_WINDOW_OPTIONS,
  buildHistoryPoints,
  DEFAULT_CHANGE_WINDOW,
  formatHistoryValue,
  hasRejectedTime,
  HISTORY_METRIC_LABELS,
  historyMetricValue,
  summarizeClient,
  type ClientHistorySummary,
  type HistoryMetric,
} from '@/utils/testHistory'

const RUNS_PER_CLIENT_OPTIONS = [25, 50, 100, 250, 500, 1000] as const
const DEFAULT_RUNS_PER_CLIENT = 100
const TABLE_PAGE_SIZE = 50

interface TestHistorySearch {
  test?: string
  n?: number
  metric?: HistoryMetric
  x?: HistoryXAxis
  steps?: string
  // Comma-separated clients to show. All clients show when it is not set.
  clients?: string
  // The number of recent runs per client that the step search looks at.
  w?: number
}

function parseStepFilter(param: string | undefined): IndexStepType[] {
  if (!param) return DEFAULT_INDEX_STEP_FILTER
  const steps = param.split(',').filter((s): s is IndexStepType => ALL_INDEX_STEP_TYPES.includes(s as IndexStepType))
  return steps.length > 0 ? steps : DEFAULT_INDEX_STEP_FILTER
}

// Serialize step filter to URL param (undefined if default)
function serializeStepFilter(steps: IndexStepType[]): string | undefined {
  const sorted = [...steps].sort()
  const defaultSorted = [...DEFAULT_INDEX_STEP_FILTER].sort()
  if (sorted.length === defaultSorted.length && sorted.every((s, i) => s === defaultSorted[i])) {
    return undefined
  }
  return steps.join(',')
}

// ChangeBadge colors the change since the latest step with the rule of the
// suite's regressions tab: red or green only when the change is significant.
function ChangeBadge({
  summary,
  metric,
  stepWindow,
}: {
  summary: ClientHistorySummary
  metric: HistoryMetric
  stepWindow: number
}) {
  const { baseline, since } = summary
  if (baseline === undefined || since === undefined) {
    return <span className="text-xs/5 text-gray-400 dark:text-gray-500">not enough runs</span>
  }

  const { change } = baseline
  const direction = classifyChange(baseline, metric, DEFAULT_REGRESSION_THRESHOLD)
  const sinceLabel =
    baseline.stepRuns === 1
      ? 'The latest run'
      : `The ${baseline.stepRuns} runs since ${new Date(since.runStart * 1000).toLocaleString()}`

  return (
    <span
      className={clsx(
        'rounded-xs px-1.5 py-0.5 font-mono text-xs/5 font-medium',
        !direction && 'bg-gray-100 text-gray-600 dark:bg-gray-700 dark:text-gray-300',
        direction === 'improvement' && 'bg-green-100 text-green-800 dark:bg-green-900/50 dark:text-green-300',
        direction === 'regression' && 'bg-red-100 text-red-800 dark:bg-red-900/50 dark:text-red-300',
      )}
      title={`${sinceLabel}, against the median of the ${baseline.runs} runs before (up to ${BASELINE_RUNS}). The step search looks at the last ${stepWindow} runs. Normal spread ±${baseline.noise.toFixed(1)}%, so a change counts from ${significantChange(baseline, DEFAULT_REGRESSION_THRESHOLD).toFixed(1)}%.`}
    >
      {direction === 'regression' && 'Regression '}
      {change > 0 ? '+' : ''}
      {change.toFixed(1)}%
    </span>
  )
}

function ClientSummaryCard({
  summary,
  metric,
  stepWindow,
  hidden,
  onToggle,
}: {
  summary: ClientHistorySummary
  metric: HistoryMetric
  stepWindow: number
  hidden: boolean
  onToggle: () => void
}) {
  return (
    <button
      type="button"
      onClick={onToggle}
      aria-pressed={!hidden}
      title={hidden ? 'Show this client' : 'Hide this client'}
      className={clsx(
        'flex flex-col gap-2 rounded-sm bg-white p-3 text-left shadow-xs transition-opacity hover:ring-2 hover:ring-gray-300 dark:bg-gray-800 dark:hover:ring-gray-600',
        hidden && 'opacity-40',
      )}
    >
      <div className="flex items-center justify-between gap-2">
        <ClientBadge client={summary.client} />
        <ChangeBadge summary={summary} metric={metric} stepWindow={stepWindow} />
      </div>
      <div className="flex flex-col">
        <span className="text-xs/5 text-gray-500 dark:text-gray-400">Latest</span>
        <span className="font-mono text-lg/7 font-semibold text-gray-900 dark:text-gray-100">
          {formatHistoryValue(summary.latest.value, metric)}
        </span>
        {summary.latest.image && (
          <span className="truncate font-mono text-xs/5 text-gray-500 dark:text-gray-400" title={summary.latest.image}>
            {summary.latest.image}
          </span>
        )}
      </div>
      <dl className="grid grid-cols-3 gap-2 text-xs/5">
        {(
          [
            ['Median', summary.median],
            ['Min', summary.min],
            ['Max', summary.max],
          ] as const
        ).map(([label, value]) => (
          <div key={label} className="flex flex-col">
            <dt className="text-gray-500 dark:text-gray-400">{label}</dt>
            <dd className="font-mono text-gray-900 dark:text-gray-100">
              {metric === 'mgas' ? value.toFixed(1) : formatHistoryValue(value, metric)}
            </dd>
          </div>
        ))}
      </dl>
      <span className="text-xs/5 text-gray-500 dark:text-gray-400">{summary.count} runs</span>
    </button>
  )
}

export function TestHistoryPage() {
  const { suiteHash } = useParams({ from: '/suites/$suiteHash/history' })
  const navigate = useNavigate()
  const search = useSearch({ from: '/suites/$suiteHash/history' }) as TestHistorySearch
  const testName = search.test
  const runsPerClient = RUNS_PER_CLIENT_OPTIONS.includes(search.n as (typeof RUNS_PER_CLIENT_OPTIONS)[number])
    ? search.n!
    : DEFAULT_RUNS_PER_CLIENT
  // Run # is the default: it spaces the runs evenly, so a gap of days between
  // runs does not squeeze the recent runs.
  const xAxis: HistoryXAxis = search.x === 'time' ? 'time' : 'runCount'
  const stepWindow = CHANGE_WINDOW_OPTIONS.includes(search.w as (typeof CHANGE_WINDOW_OPTIONS)[number])
    ? search.w!
    : DEFAULT_CHANGE_WINDOW
  const stepFilter = parseStepFilter(search.steps)

  const { data: suite } = useSuite(suiteHash)
  const { data: history, isLoading, error, refetch } = useTestHistory(suiteHash, testName, runsPerClient)

  const [tablePage, setTablePage] = useState(1)

  const [isDark, setIsDark] = useState(() => {
    if (typeof window === 'undefined') return false
    return document.documentElement.classList.contains('dark')
  })

  useEffect(() => {
    const observer = new MutationObserver(() => {
      setIsDark(document.documentElement.classList.contains('dark'))
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [])

  const durations = useMemo(() => history?.durations ?? [], [history])
  const showRejected = hasRejectedTime(durations)
  // A reject time metric left in a link to a test without one falls back to MGas/s.
  const metric: HistoryMetric =
    search.metric === 'duration' || (search.metric === 'rejected' && showRejected) ? search.metric : 'mgas'

  const pointsByClient = useMemo(() => buildHistoryPoints(durations, metric, stepFilter), [durations, metric, stepFilter])

  const summaries = useMemo(
    () =>
      [...pointsByClient.entries()]
        .map(([client, points]) => summarizeClient(client, points, stepWindow))
        .filter((s): s is ClientHistorySummary => s !== undefined),
    [pointsByClient, stepWindow],
  )

  // Clients hidden by the clients search param. The summary cards still show
  // them, dimmed, so the user can add them back.
  const hiddenClients = useMemo(() => {
    const shown = search.clients?.split(',').filter(Boolean) ?? []
    if (shown.length === 0) return new Set<string>()
    return new Set([...pointsByClient.keys()].filter((client) => !shown.includes(client)))
  }, [search.clients, pointsByClient])

  const visiblePoints = useMemo(
    () => new Map([...pointsByClient.entries()].filter(([client]) => !hiddenClients.has(client))),
    [pointsByClient, hiddenClients],
  )

  // The API returns the newest run first; stats.json is sorted the same way by the hook.
  const tableRows = useMemo(() => durations.filter((d) => !hiddenClients.has(d.client)), [durations, hiddenClients])
  const totalTablePages = Math.max(1, Math.ceil(tableRows.length / TABLE_PAGE_SIZE))
  const currentTablePage = Math.min(tablePage, totalTablePages)
  const pagedRows = tableRows.slice((currentTablePage - 1) * TABLE_PAGE_SIZE, currentTablePage * TABLE_PAGE_SIZE)

  const testIndex = suite?.tests?.findIndex((t) => t.name === testName) ?? -1
  const testNumber = testIndex >= 0 ? testIndex + 1 : undefined

  const updateSearch = (patch: Partial<TestHistorySearch>) => {
    navigate({
      to: '/suites/$suiteHash/history',
      params: { suiteHash },
      search: { ...search, ...patch },
      replace: true,
    })
  }

  const toggleClient = (client: string) => {
    const shown = [...pointsByClient.keys()].filter((c) => (c === client ? hiddenClients.has(c) : !hiddenClients.has(c)))
    setTablePage(1)
    // Hiding the last client, or showing all of them, clears the filter.
    updateSearch({ clients: shown.length === 0 || shown.length === pointsByClient.size ? undefined : shown.join(',') })
  }

  const openRun = (runId: string) => {
    navigate({ to: '/runs/$runId', params: { runId }, search: { testModal: testName } })
  }

  const metricOptions: { value: HistoryMetric; label: string }[] = [
    { value: 'mgas', label: HISTORY_METRIC_LABELS.mgas },
    { value: 'duration', label: HISTORY_METRIC_LABELS.duration },
    ...(showRejected ? [{ value: 'rejected' as const, label: HISTORY_METRIC_LABELS.rejected }] : []),
  ]

  const suiteName = suite?.metadata?.labels?.name ?? suiteHash

  return (
    <div className="flex flex-col gap-6">
      <div className="flex min-w-0 items-center gap-2 text-sm/6 text-gray-500 dark:text-gray-400">
        <Link to="/suites" className="shrink-0 hover:text-gray-700 dark:hover:text-gray-300">
          Suites
        </Link>
        <span>/</span>
        <Link
          to="/suites/$suiteHash"
          params={{ suiteHash }}
          className="truncate font-mono hover:text-gray-700 dark:hover:text-gray-300"
        >
          {suiteName}
        </Link>
        <span>/</span>
        <span className="shrink-0 text-gray-900 dark:text-gray-100">Test history</span>
      </div>

      {!testName ? (
        <EmptyState title="No test selected" message="Open the history from a test on the suite or run page." />
      ) : (
        <>
          <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
            <div className="flex min-w-0 items-start gap-3">
              <JDenticon value={suiteHash} size={40} className="shrink-0 rounded-xs" />
              <div className="flex min-w-0 flex-col">
                <span className="text-xs/5 font-medium text-gray-500 uppercase dark:text-gray-400">
                  Test history{testNumber !== undefined && ` · Test #${testNumber}`}
                </span>
                <div className="text-sm/6 text-gray-900 dark:text-gray-100">
                  <TestName name={testName} showCopy showRawBelow />
                </div>
              </div>
            </div>
            {testNumber !== undefined && (
              <Link
                to="/suites/$suiteHash"
                params={{ suiteHash }}
                search={{ tab: 'tests', detail: testNumber }}
                className="flex shrink-0 items-center gap-1 text-sm/6 text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
              >
                Test details
                <ExternalLink className="size-4" />
              </Link>
            )}
          </div>

          <div className="flex flex-wrap items-center gap-x-6 gap-y-2 rounded-xs bg-white p-2 shadow-xs sm:p-3 dark:bg-gray-800">
            <div className="flex items-center gap-2">
              <span className="text-xs/5 font-medium text-gray-700 sm:text-sm/6 dark:text-gray-300">Metric:</span>
              <SegmentedControl
                ariaLabel="Metric"
                value={metric}
                options={metricOptions}
                onChange={(value) => updateSearch({ metric: value === 'mgas' ? undefined : value })}
              />
            </div>
            <div className="flex items-center gap-2">
              <span className="text-xs/5 font-medium text-gray-700 sm:text-sm/6 dark:text-gray-300">Steps:</span>
              <div className="flex items-center gap-1">
                {ALL_INDEX_STEP_TYPES.map((step) => (
                  <button
                    key={step}
                    type="button"
                    onClick={() => {
                      const next = stepFilter.includes(step) ? stepFilter.filter((s) => s !== step) : [...stepFilter, step]
                      if (next.length > 0) updateSearch({ steps: serializeStepFilter(next) })
                    }}
                    className={clsx(
                      'rounded-xs px-2 py-0.5 text-xs font-medium capitalize transition-colors sm:px-2.5 sm:py-1',
                      stepFilter.includes(step)
                        ? 'bg-blue-100 text-blue-700 dark:bg-blue-900/50 dark:text-blue-300'
                        : 'bg-gray-100 text-gray-400 dark:bg-gray-700 dark:text-gray-500',
                    )}
                    title={`${stepFilter.includes(step) ? 'Exclude' : 'Include'} ${step} step in the metric`}
                  >
                    {step}
                  </button>
                ))}
              </div>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-xs/5 font-medium text-gray-700 sm:text-sm/6 dark:text-gray-300">X axis:</span>
              <SegmentedControl
                ariaLabel="X axis"
                value={xAxis}
                options={[
                  { value: 'runCount', label: 'Run #' },
                  { value: 'time', label: 'Time' },
                ]}
                onChange={(value) => updateSearch({ x: value === 'runCount' ? undefined : value })}
              />
            </div>
            <label className="flex items-center gap-2">
              <span className="text-xs/5 font-medium text-gray-700 sm:text-sm/6 dark:text-gray-300">Runs per client:</span>
              <select
                value={runsPerClient}
                onChange={(e) => {
                  const n = Number(e.target.value)
                  setTablePage(1)
                  updateSearch({ n: n === DEFAULT_RUNS_PER_CLIENT ? undefined : n })
                }}
                className="rounded-xs border border-gray-300 bg-white px-2 py-1 text-xs/5 text-gray-900 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100"
              >
                {RUNS_PER_CLIENT_OPTIONS.map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            </label>
          </div>

          {isLoading ? (
            <LoadingState message="Loading test history..." />
          ) : error ? (
            <ErrorState message={error.message} retry={() => refetch()} />
          ) : durations.length === 0 ? (
            <EmptyState title="No runs" message="No run of this suite has a result for this test." />
          ) : (
            <>
              {summaries.length > 0 && (
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
                  {summaries.map((summary) => (
                    <ClientSummaryCard
                      key={summary.client}
                      summary={summary}
                      stepWindow={stepWindow}
                      metric={metric}
                      hidden={hiddenClients.has(summary.client)}
                      onToggle={() => toggleClient(summary.client)}
                    />
                  ))}
                </div>
              )}

              <Card title={`${HISTORY_METRIC_LABELS[metric]} over ${xAxis === 'time' ? 'time' : 'runs'}`}>
                {visiblePoints.size === 0 ? (
                  <p className="py-8 text-center text-sm/6 text-gray-500 dark:text-gray-400">
                    No {HISTORY_METRIC_LABELS[metric]} data for the selected clients and steps.
                  </p>
                ) : (
                  <TestHistoryChart
                    pointsByClient={visiblePoints}
                    metric={metric}
                    xAxis={xAxis}
                    isDark={isDark}
                    onRunClick={openRun}
                  />
                )}
              </Card>

              <Card title="Runs" headerExtra={<span className="text-xs/5 text-gray-500 dark:text-gray-400">{tableRows.length} runs</span>}>
                <div className="flex flex-col gap-4">
                  <div className="overflow-x-auto">
                    <table className="min-w-full text-sm/6">
                      <thead>
                        <tr className="border-b border-gray-200 text-left text-xs/5 font-medium text-gray-500 uppercase dark:border-gray-700 dark:text-gray-400">
                          <th className="px-2 py-2">Time</th>
                          <th className="px-2 py-2">Client</th>
                          <th className="px-2 py-2">Image</th>
                          <th className="px-2 py-2 text-right">MGas/s</th>
                          <th className="px-2 py-2 text-right">Duration</th>
                          {showRejected && <th className="px-2 py-2 text-right">Reject time</th>}
                          <th className="px-2 py-2 text-right">Gas used</th>
                          <th className="px-2 py-2" />
                        </tr>
                      </thead>
                      <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
                        {pagedRows.map((d) => {
                          const mgas = historyMetricValue(d, 'mgas', stepFilter)
                          const duration = historyMetricValue(d, 'duration', stepFilter)
                          const rejected = historyMetricValue(d, 'rejected', stepFilter)
                          const { gasUsed } = getRunDurationAggregatedStats(d, stepFilter)
                          return (
                            <tr key={d.id} className="hover:bg-gray-50 dark:hover:bg-gray-700/50">
                              <td className="px-2 py-1.5 whitespace-nowrap text-gray-700 dark:text-gray-300">
                                {new Date(d.run_start * 1000).toLocaleString()}
                              </td>
                              <td className="px-2 py-1.5">
                                <ClientBadge client={d.client} />
                              </td>
                              <td
                                className="max-w-2xs truncate px-2 py-1.5 font-mono text-xs/5 text-gray-500 dark:text-gray-400"
                                title={d.image}
                              >
                                {d.image ?? '-'}
                              </td>
                              <td className="px-2 py-1.5 text-right font-mono text-gray-900 dark:text-gray-100">
                                {mgas !== undefined ? mgas.toFixed(2) : '-'}
                              </td>
                              <td className="px-2 py-1.5 text-right font-mono text-gray-900 dark:text-gray-100">
                                {duration !== undefined ? formatHistoryValue(duration, 'duration') : '-'}
                              </td>
                              {showRejected && (
                                <td className="px-2 py-1.5 text-right font-mono text-gray-900 dark:text-gray-100">
                                  {rejected !== undefined ? formatHistoryValue(rejected, 'rejected') : '-'}
                                </td>
                              )}
                              <td className="px-2 py-1.5 text-right font-mono text-gray-500 dark:text-gray-400">
                                {formatNumber(gasUsed)}
                              </td>
                              <td className="px-2 py-1.5 text-right">
                                <Link
                                  to="/runs/$runId"
                                  params={{ runId: d.id }}
                                  search={{ testModal: testName }}
                                  className="text-xs/5 text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                                >
                                  Open run
                                </Link>
                              </td>
                            </tr>
                          )
                        })}
                      </tbody>
                    </table>
                  </div>
                  <Pagination currentPage={currentTablePage} totalPages={totalTablePages} onPageChange={setTablePage} />
                </div>
              </Card>
            </>
          )}
        </>
      )}
    </div>
  )
}
