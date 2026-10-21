import { Fragment, useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQueries } from '@tanstack/react-query'
import clsx from 'clsx'
import { ChevronRight, GitCompare, X } from 'lucide-react'
import { fetchRunConfig, runConfigQueryKey } from '@/api/hooks/useRunConfig'
import { getRunDurationAggregatedStats, type IndexStepType, type TestHistoryDuration } from '@/api/types'
import { ConfigDiff } from '@/components/compare/ConfigDiff'
import { MAX_COMPARE_RUNS, MIN_COMPARE_RUNS } from '@/components/compare/constants'
import { PreviousRunDiff } from '@/components/compare/PreviousRunDiff'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { Pagination } from '@/components/shared/Pagination'
import { Spinner } from '@/components/shared/Spinner'
import { formatNumber } from '@/utils/format'
import {
  formatHistoryValue,
  historyMetricValue,
  previousRuns,
  sortHistoryRuns,
  type HistorySortColumn,
  type SortDirection,
} from '@/utils/testHistory'

const PAGE_SIZE = 50

interface HistoryRunsTableProps {
  // The runs of the shown clients, in any order.
  runs: TestHistoryDuration[]
  // All runs of the test history. The previous run of a client comes from
  // here, so a hidden client does not change it.
  allRuns: TestHistoryDuration[]
  suiteHash: string
  testName: string
  stepFilter: IndexStepType[]
  // The steps search param, passed on to the compare page.
  steps?: string
  showRejected: boolean
  // The runs per client of the test history.
  historyRuns: number
  sort: HistorySortColumn
  sortDir: SortDirection
  onSortChange: (sort: HistorySortColumn, sortDir: SortDirection) => void
}

function SortIcon({ direction, active }: { direction: SortDirection; active: boolean }) {
  return (
    <svg
      className={clsx('ml-1 inline-block size-3', active ? 'text-gray-700 dark:text-gray-300' : 'text-gray-400')}
      viewBox="0 0 12 12"
      fill="currentColor"
    >
      {direction === 'asc' ? <path d="M6 2L10 8H2L6 2Z" /> : <path d="M6 10L2 4H10L6 10Z" />}
    </svg>
  )
}

function SortableHeader({
  label,
  column,
  sort,
  sortDir,
  onSort,
  align = 'left',
}: {
  label: string
  column: HistorySortColumn
  sort: HistorySortColumn
  sortDir: SortDirection
  onSort: (column: HistorySortColumn) => void
  align?: 'left' | 'right'
}) {
  const active = sort === column
  return (
    <th
      aria-sort={active ? (sortDir === 'asc' ? 'ascending' : 'descending') : undefined}
      className={clsx('px-2 py-2', align === 'right' && 'text-right')}
    >
      <button
        type="button"
        onClick={() => onSort(column)}
        className="inline-flex items-center uppercase hover:text-gray-700 dark:hover:text-gray-300"
      >
        {label}
        <SortIcon direction={active ? sortDir : 'desc'} active={active} />
      </button>
    </th>
  )
}

function formatRunStart(runStart: number): string {
  return new Date(runStart * 1000).toLocaleString()
}

// SelectedRunsDiff shows the config diff of the selected runs, oldest first.
function SelectedRunsDiff({ runs }: { runs: TestHistoryDuration[] }) {
  const [onlyDifferences, setOnlyDifferences] = useState(true)
  const configs = useQueries({
    queries: runs.map((d) => ({
      queryKey: runConfigQueryKey(d.id),
      queryFn: () => fetchRunConfig(d.id),
      retry: 1,
    })),
  })

  if (configs.some((q) => q.isLoading)) {
    return (
      <div className="flex items-center gap-2 py-2 text-xs/5 text-gray-500 dark:text-gray-400">
        <Spinner size="sm" /> Loading the run configs...
      </div>
    )
  }

  const missing = runs.filter((_, i) => !configs[i].data)
  if (missing.length > 0) {
    return (
      <p className="py-2 text-xs/5 text-gray-500 dark:text-gray-400">
        The config of {missing.map((d) => `${d.client} · ${formatRunStart(d.run_start)}`).join(', ')} is not available.
      </p>
    )
  }

  return (
    <div className="flex flex-col gap-2">
      <label className="flex items-center gap-2 self-end text-xs/5 text-gray-700 dark:text-gray-300">
        <input
          type="checkbox"
          checked={onlyDifferences}
          onChange={(e) => setOnlyDifferences(e.target.checked)}
          className="rounded-xs border-gray-300 dark:border-gray-600"
        />
        Only differences
      </label>
      <ConfigDiff
        runs={runs.map((d, i) => ({ runId: d.id, config: configs[i].data!, result: null, index: i }))}
        labelMode="default"
        labels={runs.map((d) => `${d.client} · ${formatRunStart(d.run_start)}`)}
        onlyDifferences={onlyDifferences}
      />
    </div>
  )
}

// HistoryRunsTable lists the runs of a test history. A row opens the config
// diff to the previous run of its client. Selected rows show their config
// diff, or open on the compare page.
export function HistoryRunsTable({
  runs,
  allRuns,
  suiteHash,
  testName,
  stepFilter,
  steps,
  showRejected,
  historyRuns,
  sort,
  sortDir,
  onSortChange,
}: HistoryRunsTableProps) {
  const [page, setPage] = useState(1)
  const [openRows, setOpenRows] = useState<Set<string>>(new Set())
  const [selected, setSelected] = useState<string[]>([])
  const [showDiff, setShowDiff] = useState(false)

  const sortedRuns = useMemo(() => sortHistoryRuns(runs, sort, sortDir, stepFilter), [runs, sort, sortDir, stepFilter])
  const previous = useMemo(() => previousRuns(allRuns), [allRuns])
  const runById = useMemo(() => new Map(allRuns.map((d) => [d.id, d])), [allRuns])

  // The selected runs that are still in the history, oldest first.
  const selectedRuns = selected
    .map((id) => runById.get(id))
    .filter((d): d is TestHistoryDuration => d !== undefined)
    .sort((a, b) => a.run_start - b.run_start || a.id.localeCompare(b.id))
  const canCompare = selectedRuns.length >= MIN_COMPARE_RUNS

  const totalPages = Math.max(1, Math.ceil(sortedRuns.length / PAGE_SIZE))
  const currentPage = Math.min(page, totalPages)
  const pagedRuns = sortedRuns.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE)
  const colCount = 9 + (showRejected ? 1 : 0)

  const handleSort = (column: HistorySortColumn) => {
    setPage(1)
    // A new column starts with the largest value, or A to Z for the client.
    if (column !== sort) onSortChange(column, column === 'client' ? 'asc' : 'desc')
    else onSortChange(column, sortDir === 'asc' ? 'desc' : 'asc')
  }

  const toggleRow = (id: string) =>
    setOpenRows((rows) => {
      const next = new Set(rows)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })

  const toggleSelected = (id: string) =>
    setSelected((ids) => (ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id]))

  const clearSelection = () => {
    setSelected([])
    setShowDiff(false)
  }

  return (
    <div className="flex flex-col gap-4">
      {selectedRuns.length > 0 && (
        <div className="flex flex-col gap-3 rounded-xs border border-blue-200 bg-blue-50/50 p-3 dark:border-blue-900 dark:bg-blue-900/10">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <span className="text-xs/5 text-gray-700 sm:text-sm/6 dark:text-gray-300">
              {selectedRuns.length} of up to {MAX_COMPARE_RUNS} runs selected
              {!canCompare && ' · select one more run to compare'}
            </span>
            <div className="flex flex-wrap items-center gap-3">
              <button
                type="button"
                disabled={!canCompare}
                onClick={() => setShowDiff(!showDiff)}
                aria-expanded={showDiff && canCompare}
                className="rounded-xs bg-blue-600 px-2.5 py-1 text-xs/5 font-medium text-white hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-40 dark:bg-blue-500 dark:hover:bg-blue-600"
              >
                {showDiff && canCompare ? 'Hide config diff' : 'Config diff'}
              </button>
              {canCompare && (
                <Link
                  to="/compare"
                  search={{ runs: selectedRuns.map((d) => d.id).join(','), steps }}
                  className="inline-flex items-center gap-1 text-xs/5 font-medium text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                >
                  <GitCompare className="size-4" />
                  Compare runs
                </Link>
              )}
              <button
                type="button"
                onClick={clearSelection}
                className="inline-flex items-center gap-1 text-xs/5 text-gray-500 hover:text-gray-900 dark:text-gray-400 dark:hover:text-gray-100"
              >
                <X className="size-4" />
                Clear
              </button>
            </div>
          </div>
          {showDiff && canCompare && <SelectedRunsDiff runs={selectedRuns} />}
        </div>
      )}

      <div className="@container overflow-x-auto">
        <table className="min-w-full text-sm/6">
          <thead>
            <tr className="border-b border-gray-200 text-left text-xs/5 font-medium text-gray-500 uppercase dark:border-gray-700 dark:text-gray-400">
              <th className="w-0 py-2 pr-0 pl-2">
                <span className="sr-only">Config diff to the previous run</span>
              </th>
              <th className="w-0 px-2 py-2">
                <span className="sr-only">Select</span>
              </th>
              <SortableHeader label="Time" column="time" sort={sort} sortDir={sortDir} onSort={handleSort} />
              <SortableHeader label="Client" column="client" sort={sort} sortDir={sortDir} onSort={handleSort} />
              <th className="px-2 py-2">Image</th>
              <SortableHeader label="MGas/s" column="mgas" sort={sort} sortDir={sortDir} onSort={handleSort} align="right" />
              <SortableHeader
                label="Duration"
                column="duration"
                sort={sort}
                sortDir={sortDir}
                onSort={handleSort}
                align="right"
              />
              {showRejected && (
                <SortableHeader
                  label="Reject time"
                  column="rejected"
                  sort={sort}
                  sortDir={sortDir}
                  onSort={handleSort}
                  align="right"
                />
              )}
              <SortableHeader label="Gas used" column="gas" sort={sort} sortDir={sortDir} onSort={handleSort} align="right" />
              <th className="px-2 py-2" />
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
            {pagedRuns.map((d) => {
              const mgas = historyMetricValue(d, 'mgas', stepFilter)
              const duration = historyMetricValue(d, 'duration', stepFilter)
              const rejected = historyMetricValue(d, 'rejected', stepFilter)
              const { gasUsed } = getRunDurationAggregatedStats(d, stepFilter)
              const prev = previous.get(d.id)
              // The history has the image tag, not the digest. A floating tag
              // such as :master can change its image with no new tag, and the
              // config diff of the row shows that.
              const newTag = !!prev?.image && !!d.image && prev.image !== d.image
              const open = openRows.has(d.id) && !!prev
              const isSelected = selected.includes(d.id)
              const selectionFull = !isSelected && selected.length >= MAX_COMPARE_RUNS
              return (
                <Fragment key={d.id}>
                  <tr
                    className={clsx(
                      'hover:bg-gray-50 dark:hover:bg-gray-700/50',
                      isSelected && 'bg-blue-50/60 dark:bg-blue-900/20',
                      open && !isSelected && 'bg-gray-50 dark:bg-gray-700/30',
                    )}
                  >
                    <td className="py-1.5 pr-0 pl-2">
                      {prev && (
                        <button
                          type="button"
                          onClick={() => toggleRow(d.id)}
                          aria-expanded={open}
                          title={open ? 'Hide the config diff' : `Show the config diff to the previous ${d.client} run`}
                          className="rounded-xs p-1 text-gray-500 hover:bg-gray-200 hover:text-gray-900 dark:text-gray-400 dark:hover:bg-gray-600 dark:hover:text-gray-100"
                        >
                          <ChevronRight className={clsx('size-4 transition-transform', open && 'rotate-90')} />
                        </button>
                      )}
                    </td>
                    <td className="px-2 py-1.5">
                      <input
                        type="checkbox"
                        checked={isSelected}
                        disabled={selectionFull}
                        onChange={() => toggleSelected(d.id)}
                        aria-label={`Select the ${d.client} run of ${formatRunStart(d.run_start)}`}
                        title={selectionFull ? `Select up to ${MAX_COMPARE_RUNS} runs` : 'Select to compare'}
                        className="rounded-xs border-gray-300 disabled:opacity-40 dark:border-gray-600"
                      />
                    </td>
                    <td className="px-2 py-1.5 whitespace-nowrap text-gray-700 dark:text-gray-300">
                      {formatRunStart(d.run_start)}
                    </td>
                    <td className="px-2 py-1.5">
                      <ClientBadge client={d.client} />
                    </td>
                    <td className="max-w-2xs px-2 py-1.5">
                      <div className="flex items-center gap-1.5">
                        <span className="truncate font-mono text-xs/5 text-gray-500 dark:text-gray-400" title={d.image}>
                          {d.image ?? '-'}
                        </span>
                        {newTag && (
                          <span
                            className="shrink-0 rounded-xs bg-amber-100 px-1.5 py-0.5 text-xs/4 font-medium text-amber-800 dark:bg-amber-900/50 dark:text-amber-300"
                            title={`The previous ${d.client} run used ${prev.image}. A floating tag such as :master can also change its image with no new tag: open the config diff of the row to see the digest.`}
                          >
                            new tag
                          </span>
                        )}
                      </div>
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
                        className="text-xs/5 whitespace-nowrap text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
                      >
                        Open run
                      </Link>
                    </td>
                  </tr>
                  {open && (
                    <tr className="bg-gray-50 dark:bg-gray-900/30">
                      <td colSpan={colCount} className="p-0">
                        <div className="sticky left-0 w-[100cqw]">
                          <PreviousRunDiff
                            suiteHash={suiteHash}
                            testName={testName}
                            client={d.client}
                            runId={d.id}
                            runStart={d.run_start}
                            previousRunId={prev.id}
                            steps={steps}
                            historyRuns={historyRuns}
                            labels={['Previous', 'This run']}
                          />
                        </div>
                      </td>
                    </tr>
                  )}
                </Fragment>
              )
            })}
          </tbody>
        </table>
      </div>
      <Pagination currentPage={currentPage} totalPages={totalPages} onPageChange={setPage} />
    </div>
  )
}
