import { Fragment, useMemo, useState } from 'react'
import { Link } from '@tanstack/react-router'
import clsx from 'clsx'
import { ChevronDown, Search } from 'lucide-react'
import type { RunConfig } from '@/api/types'
import { useIndex } from '@/api/hooks/useIndex'
import { useMempoolResults } from '@/api/hooks/useMempoolResult'
import { useRunConfigs } from '@/api/hooks/useRunConfig'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { SegmentedControl } from '@/components/shared/SegmentedControl'
import { explainText, findScenario, OUTCOME_CLASS, selectMempoolPeerRuns, type ClientMempoolResult } from './mempoolUtils'
import {
  buildBehaviourRows,
  filterBehaviourRows,
  groupBehaviourRows,
  shortValue,
  type BehaviourCell,
  type BehaviourFilter,
  type BehaviourGroup,
  type BehaviourRow,
} from './behaviours'
import { buildIssueMarkdown, cellResultText, clientVersion } from './issueMarkdown'
import { CheckChip, SeverityTag } from './CheckIcon'
import { CopyIssueButton } from './CopyIssueButton'
import { PolicyProfileCard } from './PolicyProfileCard'

const FILTER_OPTIONS: { value: BehaviourFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'differences', label: 'Differences only' },
  { value: 'problems', label: 'Findings & failures only' },
]

function cellTitle(cell: BehaviourCell): string {
  if (cell.kind === 'observation') return cell.value
  const state = cellResultText({ ...cell, expected: undefined, actual: undefined })
  if (cell.expected === undefined && cell.actual === undefined) return state
  return `${state}\nexpected ${cell.expected ?? '-'} → actual ${cell.actual ?? '-'}`
}

function Cell({ cell }: { cell: BehaviourCell | undefined }) {
  if (!cell) return <span className="text-gray-400 dark:text-gray-500">-</span>
  if (cell.kind === 'check') {
    return (
      <span title={cellTitle(cell)} className="inline-flex">
        <CheckChip state={cell.state} size="sm" title={cellTitle(cell)} />
      </span>
    )
  }
  if (cell.outcome === 'value') {
    return (
      <span title={cell.value} className="block truncate font-mono text-gray-900 dark:text-gray-100">
        {shortValue(cell.value)}
      </span>
    )
  }
  const showText = cell.value.trim().toLowerCase() !== cell.outcome
  return (
    <span title={cell.value} className="flex min-w-0 items-center gap-1.5">
      <span className={clsx('shrink-0 rounded-xs px-1 text-xs/4 font-medium', OUTCOME_CLASS[cell.outcome])}>{cell.outcome}</span>
      {showText && <span className="min-w-0 truncate font-mono text-gray-500 dark:text-gray-400">{shortValue(cell.value)}</span>}
    </span>
  )
}

interface MatrixProps {
  results: ClientMempoolResult[]
  configs: (RunConfig | undefined)[]
}

function rowMarkdown(row: BehaviourRow, { results, configs }: MatrixProps): string {
  const first = results.map((r) => findScenario(r, row.scenario)).find(Boolean)
  return buildIssueMarkdown({
    title: row.label,
    scenario: row.scenario,
    kind: row.kind,
    severity: row.severity,
    explain: row.explain,
    rows: results.map((r, i) => ({ client: r.client, runId: r.runId, version: clientVersion(configs[i]), result: cellResultText(row.cells[r.client]) })),
    params: first?.params,
  })
}

function GroupRows({ group, open, onToggle, props }: { group: BehaviourGroup; open: boolean; onToggle: () => void; props: MatrixProps }) {
  const { results } = props
  const multiScenario = new Set(group.rows.map((r) => r.scenario)).size > 1
  return (
    <tbody className="border-t border-gray-200 dark:border-gray-700">
      <tr className="cursor-pointer bg-gray-50 hover:bg-gray-100 dark:bg-gray-900/40 dark:hover:bg-gray-900/70" onClick={onToggle} aria-expanded={open}>
        <td colSpan={results.length + 1} className="px-3 py-1">
          <span className="flex items-center gap-2 text-xs/5">
            <ChevronDown className={clsx('size-4 shrink-0 text-gray-400 transition-transform', !open && '-rotate-90')} />
            <span className="font-medium text-gray-900 dark:text-gray-100">{group.label}</span>
            <span className="text-gray-500 dark:text-gray-400">{group.rows.length} rows</span>
            {group.differences > 0 && <span className="text-blue-700 dark:text-blue-300">{group.differences} differ</span>}
            {group.problems > 0 && <span className="text-amber-700 dark:text-amber-400">{group.problems} with findings</span>}
          </span>
        </td>
      </tr>
      {open &&
        group.rows.map((row, i) => (
          <Fragment key={row.id}>
            {multiScenario && (i === 0 || group.rows[i - 1].scenario !== row.scenario) && (
              <tr>
                <td colSpan={results.length + 1} className="px-3 pt-1.5 pl-9 font-mono text-xs/5 text-gray-400 dark:text-gray-500">
                  {row.scenario}
                </td>
              </tr>
            )}
            <tr className={clsx('group border-t border-gray-100 dark:border-gray-700/50', row.differs && 'bg-blue-50/40 dark:bg-blue-900/10')}>
              <td className="px-3 py-0.5 pl-9">
                <span className="flex min-w-0 items-center gap-1.5">
                  <span
                    title={[row.label, explainText(row.explain, row.kind === 'observation' ? 'observation' : 'check') ?? `${row.scenario}: ${row.key}`].join('\n\n')}
                    className={clsx('min-w-0 truncate', row.kind === 'observation' ? 'text-gray-600 dark:text-gray-400' : 'text-gray-800 dark:text-gray-200', row.explain && 'cursor-help')}
                  >
                    {row.label}
                  </span>
                  {row.severity ? <SeverityTag severity={row.severity} /> : <span className="shrink-0 text-gray-400 dark:text-gray-500">obs</span>}
                  <CopyIssueButton
                    compact
                    markdown={() => rowMarkdown(row, props)}
                    className="ml-auto opacity-0 group-hover:opacity-100 focus-visible:opacity-100"
                  />
                </span>
              </td>
              {results.map((r) => (
                <td key={r.client} className="px-3 py-0.5">
                  <Cell cell={row.cells[r.client]} />
                </td>
              ))}
            </tr>
          </Fragment>
        ))}
    </tbody>
  )
}

function Matrix(props: MatrixProps) {
  const { results, configs } = props
  const rows = useMemo(() => buildBehaviourRows(results), [results])
  const [filter, setFilter] = useState<BehaviourFilter>(() => (results.length < 2 ? 'all' : 'differences'))
  const [query, setQuery] = useState('')
  const shown = useMemo(() => groupBehaviourRows(filterBehaviourRows(rows, filter, query)), [rows, filter, query])
  // Open by default: the groups where the clients disagree. With one client
  // there are no differences: the groups with findings open.
  const [openGroups, setOpenGroups] = useState<Set<string>>(
    () => new Set(groupBehaviourRows(rows).filter((g) => (results.length < 2 ? g.problems > 0 : g.differences > 0)).map((g) => g.theme)),
  )
  const searching = query.trim() !== ''
  const totalShown = shown.reduce((n, g) => n + g.rows.length, 0)
  const allOpen = shown.every((g) => openGroups.has(g.theme))

  const toggle = (theme: string) => {
    const next = new Set(openGroups)
    if (next.has(theme)) next.delete(theme)
    else next.add(theme)
    setOpenGroups(next)
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-3">
        <SegmentedControl value={filter} onChange={setFilter} options={FILTER_OPTIONS} ariaLabel="Which rows to show" />
        <label className="relative flex min-w-48 flex-1 items-center sm:max-w-xs">
          <Search className="pointer-events-none absolute left-2 size-3.5 text-gray-400" />
          <input
            type="search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search checks and observations"
            aria-label="Search checks and observations"
            className="w-full rounded-sm border border-gray-300 bg-white py-1 pr-2 pl-7 text-xs/5 text-gray-900 placeholder:text-gray-400 focus:border-blue-500 focus:outline-hidden dark:border-gray-600 dark:bg-gray-800 dark:text-gray-100"
          />
        </label>
        <span className="text-xs/5 text-gray-500 dark:text-gray-400">
          {totalShown} of {rows.length} rows
        </span>
        <button
          type="button"
          onClick={() => setOpenGroups(allOpen ? new Set() : new Set(shown.map((g) => g.theme)))}
          className="ml-auto rounded-sm border border-gray-300 px-2.5 py-1 text-xs/5 font-medium text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-300 dark:hover:bg-gray-700/50"
        >
          {allOpen ? 'Collapse all' : 'Expand all'}
        </button>
      </div>

      <div className="max-h-[80dvh] overflow-auto rounded-xs border border-gray-200 bg-white dark:border-gray-700 dark:bg-gray-800">
        <table className="w-full min-w-4xl table-fixed text-xs/5">
          <colgroup>
            <col className="w-md" />
            {results.map((r) => (
              <col key={r.client} />
            ))}
          </colgroup>
          <thead className="sticky top-0 z-10 bg-white shadow-sm dark:bg-gray-800">
            <tr className="text-left text-xs/5 font-medium text-gray-500 dark:text-gray-400">
              <th className="px-3 py-2 align-bottom font-medium">Behaviour</th>
              {results.map((r, i) => (
                <th key={r.client} className="px-3 py-2 font-normal">
                  <div className="flex min-w-0 flex-col gap-0.5">
                    <ClientBadge client={r.client} />
                    <Link
                      to="/runs/$runId"
                      params={{ runId: r.runId }}
                      title={`${r.runId}${clientVersion(configs[i]) ? `\n${clientVersion(configs[i])}` : ''}`}
                      className="truncate text-gray-500 hover:text-blue-600 hover:underline dark:text-gray-400 dark:hover:text-blue-400"
                    >
                      {r.runId}
                    </Link>
                  </div>
                </th>
              ))}
            </tr>
          </thead>
          {shown.length === 0 ? (
            <tbody>
              <tr>
                <td colSpan={results.length + 1} className="px-3 py-3 text-sm/6 text-gray-500 dark:text-gray-400">
                  {filter === 'differences' && !searching
                    ? 'The clients agree on every check and observation.'
                    : 'No row matches the filter.'}
                </td>
              </tr>
            </tbody>
          ) : (
            shown.map((g) => (
              <GroupRows key={g.theme} group={g} open={searching || openGroups.has(g.theme)} onToggle={() => toggle(g.theme)} props={props} />
            ))
          )}
        </table>
      </div>
    </div>
  )
}

// MempoolBehaviours compares the pool behaviour of the clients on a mempool
// suite: one policy card per client, then a matrix of the checks and
// observations, with the latest completed run of each client.
export function MempoolBehaviours({ suiteHash }: { suiteHash: string }) {
  const { data: index, isLoading: indexLoading } = useIndex()
  const peers = useMemo(
    () => selectMempoolPeerRuns(index?.entries ?? [], suiteHash, '').map((e) => ({ client: e.instance.client, runId: e.run_id })),
    [index, suiteHash],
  )
  const runIds = useMemo(() => peers.map((p) => p.runId), [peers])
  const { data: peerResults, isLoading } = useMempoolResults(runIds)
  const { data: configs } = useRunConfigs(runIds)

  const loaded = useMemo(() => {
    const out: { r: ClientMempoolResult; config: RunConfig | undefined }[] = []
    peers.forEach((p, i) => {
      const result = peerResults[i]
      if (result) out.push({ r: { client: p.client, runId: p.runId, result }, config: configs[i] })
    })
    return out
  }, [peers, peerResults, configs])
  const results = useMemo(() => loaded.map((l) => l.r), [loaded])
  const loadedConfigs = useMemo(() => loaded.map((l) => l.config), [loaded])

  if (indexLoading || isLoading) {
    return <p className="text-sm/6 text-gray-500 dark:text-gray-400">Loading the latest run of each client...</p>
  }
  if (results.length === 0) {
    return <p className="text-sm/6 text-gray-500 dark:text-gray-400">No completed mempool run on this suite yet.</p>
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-1">
        <h2 className="text-base/7 font-semibold text-gray-900 dark:text-gray-100">Pool policy per client</h2>
        <p className="text-xs/5 text-gray-500 dark:text-gray-400">
          From the latest completed run of each client on this suite. Hover a field for what it means.
        </p>
      </div>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
        {results.map((r) => (
          <PolicyProfileCard key={r.client} client={r.client} result={r.result} runId={r.runId} />
        ))}
      </div>
      <div className="flex flex-col gap-1">
        <h2 className="text-base/7 font-semibold text-gray-900 dark:text-gray-100">Behaviours</h2>
        <p className="text-xs/5 text-gray-500 dark:text-gray-400">
          Each row is a check or an observation (invariants are left out). Rows where the clients disagree have a blue tint.
          Hover a row name for its explanation.
        </p>
      </div>
      {results.length < 2 && (
        <p className="text-xs/5 text-gray-500 dark:text-gray-400">Only one client has run this suite. Differences need at least two.</p>
      )}
      <Matrix results={results} configs={loadedConfigs} />
    </div>
  )
}
