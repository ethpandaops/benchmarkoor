import { useEffect, useRef, useState } from 'react'
import clsx from 'clsx'
import { ArrowDown, ArrowRight, ArrowUp, Check, Clock, ShieldCheck, TriangleAlert, X } from 'lucide-react'
import type { MempoolAssertion, MempoolMetric, MempoolObservation, MempoolScenarioResult, MempoolScenarioStatus } from '@/api/types'
import { Badge } from '@/components/shared/Badge'
import { Card } from '@/components/shared/Card'
import { formatDurationMs } from '@/utils/format'
import {
  checkState,
  countChecks,
  explainText,
  formatMempoolValue,
  formatParam,
  groupMetrics,
  orderChecks,
  SEVERITY_HELP,
  splitValueUnit,
  type CheckCounts,
} from './mempoolUtils'
import { isInvariant } from './behaviours'
import { OPEN_SCENARIO_EVENT, scenarioDomId, scenarioHeadlines } from './runOverview'
import { buildIssueMarkdown, cellResultText } from './issueMarkdown'
import { CheckIcon, SeverityTag } from './CheckIcon'
import { CopyIssueButton } from './CopyIssueButton'
import { ExplainButton, Explanation } from './Explain'
import { LatencyTable } from './LatencyTable'
import { PoolTimelineChart, type TimelineMark } from './PoolTimelineChart'

const STATUS_VARIANT: Record<MempoolScenarioStatus, 'success' | 'error' | 'warning'> = {
  passed: 'success',
  failed: 'error',
  error: 'warning',
}

export function ScenarioStatusBadge({ status }: { status: MempoolScenarioStatus | undefined }) {
  if (!status) return <span className="text-gray-400 dark:text-gray-500">-</span>
  return <Badge variant={STATUS_VARIANT[status] ?? 'default'}>{status}</Badge>
}

// Passed checks above this count hide behind a "Show N passed checks" button.
const MAX_VISIBLE_PASSES = 6

// IssueContext is the run a "Copy as issue" body names.
export interface IssueContext {
  client: string
  runId: string
  version?: string
}

function Section({ title, children, aside }: { title: string; children: React.ReactNode; aside?: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-baseline justify-between gap-3">
        <h4 className="text-xs/5 font-medium tracking-wide text-gray-500 uppercase dark:text-gray-400">{title}</h4>
        {aside}
      </div>
      {children}
    </section>
  )
}

// HEADER_COL is the fixed width of each summary column in a scenario header,
// so the columns line up across the cards like a table.
const HEADER_COL = 'w-24 shrink-0'

// CheckCount is a compact "Hard 5/5" line for the card header. Hard and Soft
// stack in one column; a missing count keeps its line, so headers keep one
// height.
function CheckCount({ label, count, failState }: { label: string; count: CheckCounts['hard']; failState: 'hard' | 'soft' }) {
  if (count.total === 0) return <span className="h-4" aria-hidden />
  const failed = count.total - count.passed
  const Icon = failed === 0 ? Check : failState === 'hard' ? X : TriangleAlert
  return (
    <span
      title={`${count.passed} of ${count.total} ${label.toLowerCase()} checks passed.\n\n${SEVERITY_HELP[failState]}`}
      className={clsx(
        'inline-flex cursor-help items-center gap-1 tabular-nums',
        failed === 0 && 'text-gray-500 dark:text-gray-400',
        failed > 0 && failState === 'hard' && 'font-medium text-red-700 dark:text-red-400',
        failed > 0 && failState === 'soft' && 'font-medium text-amber-700 dark:text-amber-400',
      )}
    >
      <Icon className="size-3.5" />
      {label} {count.passed}/{count.total}
    </span>
  )
}

function ExpectedActual({ assertion, prominent }: { assertion: MempoolAssertion; prominent: boolean }) {
  if (assertion.expected === undefined && assertion.actual === undefined) return null
  return (
    <div className={clsx('flex flex-wrap items-baseline gap-x-1.5 text-xs/5 break-words', prominent ? 'text-gray-700 dark:text-gray-300' : 'text-gray-400 dark:text-gray-500')}>
      <span className="font-sans">expected</span>
      <span className="font-mono">{assertion.expected ?? '-'}</span>
      <ArrowRight className="size-3 shrink-0 self-center" aria-label="actual" />
      <span className={clsx('font-mono', prominent && 'font-medium text-gray-900 dark:text-gray-100')}>{assertion.actual ?? '-'}</span>
    </div>
  )
}

function formatT(ms: number): string {
  return formatMempoolValue(ms, 'ms')
}

interface CheckRowProps {
  assertion: MempoolAssertion
  marked: boolean
  onMark?: (a: MempoolAssertion) => void
  issue?: () => string
}

function CheckRow({ assertion, marked, onMark, issue }: CheckRowProps) {
  const state = checkState(assertion)
  const [open, setOpen] = useState(false)
  const canMark = onMark !== undefined && assertion.t_ms !== undefined
  return (
    <li
      className={clsx('flex gap-2.5 py-2', canMark && 'cursor-pointer hover:bg-gray-50 dark:hover:bg-gray-700/30', marked && 'bg-blue-50/60 dark:bg-blue-900/15')}
      onClick={canMark ? () => onMark(assertion) : undefined}
    >
      <CheckIcon state={state} className="mt-1" />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className={clsx('text-sm/6 break-words', state === 'passed' ? 'text-gray-700 dark:text-gray-300' : 'font-medium text-gray-900 dark:text-gray-100')}>
            {assertion.name}
          </span>
          <SeverityTag severity={assertion.severity} />
          {assertion.explain && <ExplainButton open={open} onClick={() => setOpen(!open)} />}
          {state !== 'passed' && issue && <CopyIssueButton markdown={issue} />}
        </div>
        <ExpectedActual assertion={assertion} prominent={state !== 'passed'} />
        {open && assertion.explain && <Explanation explain={assertion.explain} />}
      </div>
      {canMark && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation()
            onMark(assertion)
          }}
          aria-pressed={marked}
          title={marked ? 'Remove the mark from the pool timeline' : 'Mark this check on the pool timeline'}
          className={clsx(
            'inline-flex h-6 shrink-0 items-center gap-1 self-start rounded-xs px-1.5 text-xs/5 tabular-nums',
            marked
              ? 'bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-200'
              : 'text-gray-500 hover:bg-gray-100 hover:text-gray-800 dark:text-gray-400 dark:hover:bg-gray-700/60 dark:hover:text-gray-100',
          )}
        >
          <Clock className="size-3.5" />
          {formatT(assertion.t_ms!)}
        </button>
      )}
    </li>
  )
}

function ToggleButton({ onClick, icon, children }: { onClick: () => void; icon: React.ReactNode; children: React.ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="mt-1 inline-flex items-center gap-1.5 self-start rounded-xs px-1 py-0.5 text-xs/5 font-medium text-blue-600 hover:bg-blue-50 hover:text-blue-700 dark:text-blue-400 dark:hover:bg-blue-900/30 dark:hover:text-blue-300"
    >
      {icon}
      {children}
    </button>
  )
}

interface ChecksListProps {
  assertions: MempoolAssertion[]
  markedName?: string
  onMark?: (a: MempoolAssertion) => void
  issueFor?: (a: MempoolAssertion) => (() => string) | undefined
}

// ChecksList shows failed checks first. Passed invariants hide behind a
// toggle (the Invariants panel sums them up); a failed invariant stays in
// the list.
function ChecksList({ assertions, markedName, onMark, issueFor }: ChecksListProps) {
  const [showPasses, setShowPasses] = useState(false)
  const [showInvariants, setShowInvariants] = useState(false)
  const ordered = orderChecks(assertions)
  const passedInvariants = ordered.filter((a) => isInvariant(a) && a.passed)
  const main = ordered.filter((a) => !isInvariant(a) || !a.passed)
  const failing = main.filter((a) => !a.passed)
  const passing = main.filter((a) => a.passed)
  const collapsePasses = passing.length > MAX_VISIBLE_PASSES && !showPasses
  const row = (a: MempoolAssertion) => <CheckRow key={a.name} assertion={a} marked={a.name === markedName} onMark={onMark} issue={issueFor?.(a)} />

  return (
    <div className="flex flex-col">
      <ul className="divide-y divide-gray-100 dark:divide-gray-700/50">
        {failing.map(row)}
        {!collapsePasses && passing.map(row)}
        {showInvariants && passedInvariants.map(row)}
      </ul>
      <div className="flex flex-wrap gap-x-3">
        {passing.length > MAX_VISIBLE_PASSES && (
          <ToggleButton onClick={() => setShowPasses(!showPasses)} icon={<Check className="size-3.5 text-green-600 dark:text-green-400" />}>
            {showPasses ? `Hide ${passing.length} passed checks` : `Show ${passing.length} passed checks`}
          </ToggleButton>
        )}
        {passedInvariants.length > 0 && (
          <ToggleButton onClick={() => setShowInvariants(!showInvariants)} icon={<ShieldCheck className="size-3.5 text-green-600 dark:text-green-400" />}>
            {showInvariants ? `Hide ${passedInvariants.length} invariant checks` : `Show ${passedInvariants.length} invariant checks`}
          </ToggleButton>
        )}
      </div>
    </div>
  )
}

function MetricTile({ metric, label, open, onToggle }: { metric: MempoolMetric; label: string; open: boolean; onToggle: () => void }) {
  const { value, unit } = splitValueUnit(formatMempoolValue(metric.value, metric.unit))
  const Arrow = metric.better === 'lower' ? ArrowDown : metric.better === 'higher' ? ArrowUp : undefined
  return (
    <div
      title={explainText(metric.explain, 'metric') ?? label}
      className={clsx('flex flex-col justify-between gap-1 rounded-sm bg-gray-50 px-3 py-2.5 dark:bg-gray-700/40', open && 'ring-1 ring-blue-300 dark:ring-blue-700')}
    >
      <div className="flex items-start gap-1 text-xs/5 text-gray-500 dark:text-gray-400">
        <span className="line-clamp-2">{label}</span>
        {Arrow && (
          <span title={`${metric.better === 'lower' ? 'Lower' : 'Higher'} is better`} className="mt-1 shrink-0">
            <Arrow className="size-3 text-gray-400 dark:text-gray-500" />
          </span>
        )}
        {metric.explain && (
          <span className="ml-auto">
            <ExplainButton open={open} onClick={onToggle} kind="metric" label={false} />
          </span>
        )}
      </div>
      <div className="flex items-baseline gap-1">
        <span className="text-xl/7 font-semibold text-gray-900 tabular-nums dark:text-gray-100">{value}</span>
        {unit && <span className="text-xs/5 text-gray-500 dark:text-gray-400">{unit}</span>}
      </div>
    </div>
  )
}

// GroupHeading shows the group name. When the shared label suffix starts
// with it (e.g. "Access list (2930)"), the suffix is the heading; otherwise
// the suffix follows it in a muted color.
function GroupHeading({ heading, detail }: { heading: string; detail?: string }) {
  const merged = detail !== undefined && detail.toLowerCase().startsWith(heading.toLowerCase().split(' ')[0])
  return (
    <span className="text-xs/5 font-medium text-gray-700 dark:text-gray-300">
      {merged ? detail : heading}
      {detail && !merged && <span className="ml-1.5 font-normal text-gray-400 dark:text-gray-500">{detail}</span>}
    </span>
  )
}

// Large-screen columns so a group of 2 or 3 tiles fills the row.
const LG_COLS: Record<number, string> = { 2: 'lg:grid-cols-2', 3: 'lg:grid-cols-3' }

function MetricsGrid({ metrics }: { metrics: MempoolMetric[] }) {
  const groups = groupMetrics(metrics)
  const [openKey, setOpenKey] = useState<string | null>(null)
  return (
    <div className="flex flex-col gap-3">
      {groups.map((g) => {
        const open = g.items.find((item) => item.metric.key === openKey)
        return (
          <div key={g.key} className="flex flex-col gap-1.5">
            {g.heading && <GroupHeading heading={g.heading} detail={g.detail} />}
            <div className={clsx('grid grid-cols-2 gap-2', LG_COLS[g.items.length] ?? 'lg:grid-cols-4')}>
              {g.items.map((item) => (
                <MetricTile
                  key={item.metric.key}
                  metric={item.metric}
                  label={item.label}
                  open={item.metric.key === openKey}
                  onToggle={() => setOpenKey(item.metric.key === openKey ? null : item.metric.key)}
                />
              ))}
            </div>
            {open?.metric.explain && <Explanation explain={open.metric.explain} kind="metric" title={open.metric.label} />}
          </div>
        )
      })}
    </div>
  )
}

function ObservationRow({ o }: { o: MempoolObservation }) {
  const [open, setOpen] = useState(false)
  return (
    <div className="grid grid-cols-1 gap-x-4 py-1.5 sm:grid-cols-3">
      <dt className="flex items-start justify-between gap-2 text-gray-600 dark:text-gray-400" title={o.key}>
        <span>{o.label}</span>
        {o.explain && <ExplainButton open={open} onClick={() => setOpen(!open)} kind="observation" />}
      </dt>
      <dd className="font-mono text-xs/6 break-words text-gray-900 sm:col-span-2 dark:text-gray-100">{o.value}</dd>
      {open && o.explain && (
        <div className="sm:col-span-3">
          <Explanation explain={o.explain} kind="observation" />
        </div>
      )}
    </div>
  )
}

function ObservationsList({ observations }: { observations: MempoolObservation[] }) {
  return (
    <dl className="divide-y divide-gray-100 text-sm/6 dark:divide-gray-700/50">
      {observations.map((o) => (
        <ObservationRow key={o.key} o={o} />
      ))}
    </dl>
  )
}

function ParamsLine({ params }: { params: Record<string, unknown> }) {
  return (
    <p className="flex flex-wrap gap-x-4 gap-y-1 font-mono text-xs/5 text-gray-600 dark:text-gray-400">
      {Object.entries(params).map(([k, v]) => (
        <span key={k} className="break-words">
          <span className="text-gray-400 dark:text-gray-500">{k}=</span>
          {formatParam(v)}
        </span>
      ))}
    </p>
  )
}

// Headlines are the 1 or 2 key values of a scenario in its header. The
// column has a fixed width so the headers line up.
function Headlines({ scenario }: { scenario: MempoolScenarioResult }) {
  const items = scenarioHeadlines(scenario)
  return (
    <span className="flex w-28 shrink-0 flex-col items-end text-xs/4 text-gray-700 tabular-nums dark:text-gray-300">
      {items.map((h) => (
        <span key={h.label} title={h.label} className="max-w-full cursor-help truncate">
          {h.text}
        </span>
      ))}
    </span>
  )
}

interface ScenarioCardProps {
  scenario: MempoolScenarioResult
  // The checks and observations to show; default all. The run page filter
  // passes a subset.
  assertions?: MempoolAssertion[]
  observations?: MempoolObservation[]
  // forceOpen opens the card when it turns true (e.g. a filter is on).
  forceOpen?: boolean
  issueContext?: IssueContext
}

export function ScenarioCard({ scenario, assertions: shownAssertions, observations: shownObservations, forceOpen = false, issueContext }: ScenarioCardProps) {
  const metrics = scenario.metrics ?? []
  const latencies = scenario.latencies ?? []
  const allAssertions = scenario.assertions ?? []
  const assertions = shownAssertions ?? allAssertions
  const observations = shownObservations ?? scenario.observations ?? []
  const counts = countChecks(allAssertions)
  const params = scenario.params && Object.keys(scenario.params).length > 0 ? scenario.params : undefined
  const domId = scenarioDomId(scenario.name)

  const [collapsed, setCollapsed] = useState(() => !forceOpen && scenario.status === 'passed' && window.location.hash !== `#${domId}`)
  const [prevForce, setPrevForce] = useState(forceOpen)
  if (forceOpen !== prevForce) {
    setPrevForce(forceOpen)
    if (forceOpen) setCollapsed(false)
  }

  // A link elsewhere on the page (e.g. the problems banner) opens the card
  // and scrolls to it.
  useEffect(() => {
    const onOpen = (e: Event) => {
      if ((e as CustomEvent<string>).detail !== scenario.name) return
      setCollapsed(false)
      window.requestAnimationFrame(() => document.getElementById(domId)?.scrollIntoView({ behavior: 'smooth', block: 'start' }))
    }
    window.addEventListener(OPEN_SCENARIO_EVENT, onOpen)
    if (window.location.hash === `#${domId}`) document.getElementById(domId)?.scrollIntoView({ block: 'start' })
    return () => window.removeEventListener(OPEN_SCENARIO_EVENT, onOpen)
  }, [scenario.name, domId])

  const [mark, setMark] = useState<(TimelineMark & { name: string }) | null>(null)
  const timelineRef = useRef<HTMLDivElement>(null)
  const onMark = (a: MempoolAssertion) => {
    if (a.t_ms === undefined) return
    if (mark?.name === a.name) {
      setMark(null)
      return
    }
    setMark({ name: a.name, t_ms: a.t_ms, label: a.name })
    window.requestAnimationFrame(() => timelineRef.current?.scrollIntoView({ behavior: 'smooth', block: 'center' }))
  }
  const hasMarks = allAssertions.some((a) => a.t_ms !== undefined) && (scenario.timeline ?? []).length > 0

  const issueFor = issueContext
    ? (a: MempoolAssertion) => () =>
        buildIssueMarkdown({
          title: a.name,
          scenario: scenario.name,
          kind: 'check',
          severity: a.severity,
          explain: a.explain,
          rows: [{ ...issueContext, result: cellResultText({ kind: 'check', state: checkState(a), expected: a.expected, actual: a.actual }) }],
          params: scenario.params,
        })
    : undefined

  return (
    <Card
      id={domId}
      className="scroll-mt-4"
      collapsible
      collapsed={collapsed}
      onCollapsedChange={setCollapsed}
      title={<span className="font-mono">{scenario.name}</span>}
      headerExtra={
        <span className="flex items-center gap-3 text-xs/5">
          <Headlines scenario={scenario} />
          <span className={clsx(HEADER_COL, 'flex flex-col text-xs/4')}>
            <CheckCount label="Hard" count={counts.hard} failState="hard" />
            <CheckCount label="Soft" count={counts.soft} failState="soft" />
          </span>
          <span className="w-16 shrink-0 text-right text-gray-500 tabular-nums dark:text-gray-400">
            {formatDurationMs(scenario.duration_ms)}
          </span>
          <span className="flex w-20 shrink-0 justify-end">
            <ScenarioStatusBadge status={scenario.status} />
          </span>
        </span>
      }
    >
      <div className="flex flex-col gap-6">
        {scenario.description && <p className="max-w-4xl text-sm/6 text-gray-600 dark:text-gray-400">{scenario.description}</p>}
        {scenario.error && (
          <div className="rounded-xs border border-red-200 bg-red-50 px-3 py-2 font-mono text-xs/5 break-words text-red-800 dark:border-red-800 dark:bg-red-900/20 dark:text-red-200">
            {scenario.error}
          </div>
        )}
        {assertions.length > 0 && (
          <Section
            title="Checks"
            aside={hasMarks && <span className="text-xs/5 text-gray-500 dark:text-gray-400">Click a check to mark its time on the pool timeline</span>}
          >
            <ChecksList assertions={assertions} markedName={mark?.name} onMark={hasMarks ? onMark : undefined} issueFor={issueFor} />
          </Section>
        )}
        {metrics.length > 0 && (
          <Section title="Metrics">
            <MetricsGrid metrics={metrics} />
          </Section>
        )}
        <div ref={timelineRef} className="scroll-mt-4">
          <Section
            title="Pool timeline"
            aside={
              mark && (
                <button
                  type="button"
                  onClick={() => setMark(null)}
                  className="rounded-xs px-1 text-xs/5 text-blue-600 hover:bg-blue-50 dark:text-blue-400 dark:hover:bg-blue-900/30"
                >
                  Clear the mark
                </button>
              )
            }
          >
            <PoolTimelineChart timeline={scenario.timeline ?? []} events={scenario.events ?? []} blocks={scenario.blocks} marks={mark ? [mark] : undefined} />
          </Section>
        </div>
        {latencies.length > 0 && (
          <Section title="Latencies">
            <LatencyTable latencies={latencies} />
          </Section>
        )}
        {observations.length > 0 && (
          <Section title="Observations">
            <ObservationsList observations={observations} />
          </Section>
        )}
        {params && (
          <Section title="Parameters">
            <ParamsLine params={params} />
          </Section>
        )}
      </div>
    </Card>
  )
}
