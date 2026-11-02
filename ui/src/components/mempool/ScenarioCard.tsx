import { useState } from 'react'
import clsx from 'clsx'
import { ArrowDown, ArrowRight, ArrowUp, Check, TriangleAlert, X } from 'lucide-react'
import type { MempoolAssertion, MempoolMetric, MempoolObservation, MempoolScenarioResult, MempoolScenarioStatus } from '@/api/types'
import { Badge } from '@/components/shared/Badge'
import { Card } from '@/components/shared/Card'
import { formatDurationMs } from '@/utils/format'
import {
  checkState,
  countChecks,
  formatMempoolValue,
  formatParam,
  groupMetrics,
  orderChecks,
  splitValueUnit,
  type CheckCounts,
} from './mempoolUtils'
import { CheckIcon, SeverityTag } from './CheckIcon'
import { LatencyTable } from './LatencyTable'
import { PoolTimelineChart } from './PoolTimelineChart'

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

// CheckCount is a compact "Hard 5/5" summary for the card header.
function CheckCount({ label, count, failState }: { label: string; count: CheckCounts['hard']; failState: 'hard' | 'soft' }) {
  if (count.total === 0) return null
  const failed = count.total - count.passed
  const Icon = failed === 0 ? Check : failState === 'hard' ? X : TriangleAlert
  return (
    <span
      title={`${count.passed} of ${count.total} ${label.toLowerCase()} checks passed`}
      className={clsx(
        'inline-flex items-center gap-1 tabular-nums',
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

function CheckRow({ assertion }: { assertion: MempoolAssertion }) {
  const state = checkState(assertion)
  return (
    <li className="flex gap-2.5 py-2">
      <CheckIcon state={state} className="mt-1" />
      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
        <div className="flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
          <span className={clsx('text-sm/6 break-words', state === 'passed' ? 'text-gray-700 dark:text-gray-300' : 'font-medium text-gray-900 dark:text-gray-100')}>
            {assertion.name}
          </span>
          <SeverityTag severity={assertion.severity} />
        </div>
        <ExpectedActual assertion={assertion} prominent={state !== 'passed'} />
      </div>
    </li>
  )
}

function ChecksList({ assertions }: { assertions: MempoolAssertion[] }) {
  const [showPasses, setShowPasses] = useState(false)
  const ordered = orderChecks(assertions)
  const failing = ordered.filter((a) => !a.passed)
  const passing = ordered.filter((a) => a.passed)
  const collapsePasses = passing.length > MAX_VISIBLE_PASSES && !showPasses

  return (
    <div className="flex flex-col">
      <ul className="divide-y divide-gray-100 dark:divide-gray-700/50">
        {failing.map((a) => (
          <CheckRow key={a.name} assertion={a} />
        ))}
        {!collapsePasses && passing.map((a) => <CheckRow key={a.name} assertion={a} />)}
      </ul>
      {passing.length > MAX_VISIBLE_PASSES && (
        <button
          type="button"
          onClick={() => setShowPasses(!showPasses)}
          className="mt-1 inline-flex items-center gap-1.5 self-start rounded-xs px-1 py-0.5 text-xs/5 font-medium text-blue-600 hover:bg-blue-50 hover:text-blue-700 dark:text-blue-400 dark:hover:bg-blue-900/30 dark:hover:text-blue-300"
        >
          <Check className="size-3.5 text-green-600 dark:text-green-400" />
          {showPasses ? `Hide ${passing.length} passed checks` : `Show ${passing.length} passed checks`}
        </button>
      )}
    </div>
  )
}

function MetricTile({ metric, label }: { metric: MempoolMetric; label: string }) {
  const { value, unit } = splitValueUnit(formatMempoolValue(metric.value, metric.unit))
  const Arrow = metric.better === 'lower' ? ArrowDown : metric.better === 'higher' ? ArrowUp : undefined
  return (
    <div className="flex flex-col justify-between gap-1 rounded-sm bg-gray-50 px-3 py-2.5 dark:bg-gray-700/40">
      <div className="flex items-start gap-1 text-xs/5 text-gray-500 dark:text-gray-400">
        <span className="line-clamp-2" title={label}>{label}</span>
        {Arrow && (
          <span title={`${metric.better === 'lower' ? 'Lower' : 'Higher'} is better`} className="mt-1 shrink-0">
            <Arrow className="size-3 text-gray-400 dark:text-gray-500" />
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
  return (
    <div className="flex flex-col gap-3">
      {groups.map((g) => (
        <div key={g.key} className="flex flex-col gap-1.5">
          {g.heading && <GroupHeading heading={g.heading} detail={g.detail} />}
          <div className={clsx('grid grid-cols-2 gap-2', LG_COLS[g.items.length] ?? 'lg:grid-cols-4')}>
            {g.items.map((item) => (
              <MetricTile key={item.metric.key} metric={item.metric} label={item.label} />
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

function ObservationsList({ observations }: { observations: MempoolObservation[] }) {
  return (
    <dl className="divide-y divide-gray-100 text-sm/6 dark:divide-gray-700/50">
      {observations.map((o) => (
        <div key={o.key} className="grid grid-cols-1 gap-x-4 py-1.5 sm:grid-cols-3">
          <dt className="text-gray-600 dark:text-gray-400" title={o.key}>
            {o.label}
          </dt>
          <dd className="font-mono text-xs/6 break-words text-gray-900 sm:col-span-2 dark:text-gray-100">{o.value}</dd>
        </div>
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

export function ScenarioCard({ scenario }: { scenario: MempoolScenarioResult }) {
  const metrics = scenario.metrics ?? []
  const latencies = scenario.latencies ?? []
  const assertions = scenario.assertions ?? []
  const observations = scenario.observations ?? []
  const counts = countChecks(assertions)
  const params = scenario.params && Object.keys(scenario.params).length > 0 ? scenario.params : undefined

  return (
    <Card
      collapsible
      defaultCollapsed={scenario.status === 'passed'}
      title={
        <span className="flex items-center gap-2">
          <span className="font-mono">{scenario.name}</span>
          <ScenarioStatusBadge status={scenario.status} />
        </span>
      }
      headerExtra={
        <span className="flex items-center gap-4 text-xs/5">
          <CheckCount label="Hard" count={counts.hard} failState="hard" />
          <CheckCount label="Soft" count={counts.soft} failState="soft" />
          <span className="w-16 text-right text-gray-500 tabular-nums dark:text-gray-400">{formatDurationMs(scenario.duration_ms)}</span>
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
          <Section title="Checks">
            <ChecksList assertions={assertions} />
          </Section>
        )}
        {metrics.length > 0 && (
          <Section title="Metrics">
            <MetricsGrid metrics={metrics} />
          </Section>
        )}
        <Section title="Pool timeline">
          <PoolTimelineChart timeline={scenario.timeline ?? []} events={scenario.events ?? []} />
        </Section>
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
