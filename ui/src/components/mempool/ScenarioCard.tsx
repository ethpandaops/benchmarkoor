import clsx from 'clsx'
import type { MempoolAssertion, MempoolMetric, MempoolObservation, MempoolScenarioResult, MempoolScenarioStatus } from '@/api/types'
import { Badge } from '@/components/shared/Badge'
import { Card } from '@/components/shared/Card'
import { formatDurationMs } from '@/utils/format'
import { formatMempoolValue, formatParam, scenarioFindingCount } from './mempoolUtils'
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

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <h4 className="text-xs/5 font-medium uppercase tracking-wide text-gray-500 dark:text-gray-400">{title}</h4>
      {children}
    </section>
  )
}

function ParamsList({ params }: { params: Record<string, unknown> }) {
  return (
    <div className="flex flex-wrap gap-1.5">
      {Object.entries(params).map(([k, v]) => (
        <code
          key={k}
          className="rounded-xs bg-gray-100 px-1.5 py-0.5 font-mono text-xs/5 text-gray-700 dark:bg-gray-700 dark:text-gray-300"
        >
          {k}={formatParam(v)}
        </code>
      ))}
    </div>
  )
}

function MetricsGrid({ metrics }: { metrics: MempoolMetric[] }) {
  const groups = new Map<string, MempoolMetric[]>()
  for (const m of metrics) {
    const g = m.group ?? ''
    groups.set(g, [...(groups.get(g) ?? []), m])
  }

  return (
    <div className="flex flex-col gap-3">
      {[...groups.entries()].map(([group, items]) => (
        <div key={group} className="flex flex-col gap-1.5">
          {group && <span className="text-xs/5 font-medium text-gray-600 dark:text-gray-300">{group}</span>}
          <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-4">
            {items.map((m) => (
              <div key={m.key} className="rounded-xs bg-gray-50 px-3 py-2 dark:bg-gray-700/50">
                <div className="text-xs/5 text-gray-500 dark:text-gray-400" title={m.better ? `${m.better} is better` : undefined}>
                  {m.label}
                </div>
                <div className="text-sm/6 font-medium text-gray-900 dark:text-gray-100">{formatMempoolValue(m.value, m.unit)}</div>
              </div>
            ))}
          </div>
        </div>
      ))}
    </div>
  )
}

function AssertionsTable({ assertions }: { assertions: MempoolAssertion[] }) {
  return (
    <div className="overflow-x-auto">
      <table className="min-w-full divide-y divide-gray-200 text-sm/6 dark:divide-gray-700">
        <thead>
          <tr className="text-left text-xs/5 font-medium text-gray-500 dark:text-gray-400">
            <th className="py-2 pr-4">Assertion</th>
            <th className="px-2 py-2">Severity</th>
            <th className="px-2 py-2">Result</th>
            <th className="px-2 py-2">Expected</th>
            <th className="py-2 pl-2">Actual</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
          {assertions.map((a) => {
            const hardFail = a.severity === 'hard' && !a.passed
            const softFail = a.severity === 'soft' && !a.passed
            return (
              <tr
                key={a.name}
                className={clsx(
                  'text-gray-900 dark:text-gray-100',
                  hardFail && 'bg-red-50 dark:bg-red-900/20',
                  softFail && 'bg-amber-50 dark:bg-amber-900/20',
                )}
              >
                <td className="py-2 pr-4 font-mono text-xs/5">{a.name}</td>
                <td className="px-2 py-2">
                  <Badge variant={a.severity === 'hard' ? 'default' : 'warning'}>{a.severity}</Badge>
                </td>
                <td
                  className={clsx(
                    'px-2 py-2 font-medium',
                    a.passed && 'text-green-700 dark:text-green-400',
                    hardFail && 'text-red-700 dark:text-red-400',
                    softFail && 'text-amber-700 dark:text-amber-400',
                  )}
                >
                  {a.passed ? 'passed' : softFail ? 'finding' : 'failed'}
                </td>
                <td className="px-2 py-2 font-mono text-xs/5 text-gray-600 dark:text-gray-300">{a.expected ?? '-'}</td>
                <td className="py-2 pl-2 font-mono text-xs/5 text-gray-600 dark:text-gray-300">{a.actual ?? '-'}</td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}

function ObservationsTable({ observations }: { observations: MempoolObservation[] }) {
  return (
    <div className="overflow-x-auto">
      <table className="min-w-full divide-y divide-gray-200 text-sm/6 dark:divide-gray-700">
        <thead>
          <tr className="text-left text-xs/5 font-medium text-gray-500 dark:text-gray-400">
            <th className="py-2 pr-4">Observation</th>
            <th className="py-2 pl-2">Value</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
          {observations.map((o) => (
            <tr key={o.key} className="text-gray-900 dark:text-gray-100">
              <td className="py-2 pr-4" title={o.key}>{o.label}</td>
              <td className="break-all py-2 pl-2 font-mono text-xs/5 text-amber-800 dark:text-amber-300">{o.value}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export function ScenarioCard({ scenario }: { scenario: MempoolScenarioResult }) {
  const findings = scenarioFindingCount(scenario)
  const metrics = scenario.metrics ?? []
  const latencies = scenario.latencies ?? []
  const assertions = scenario.assertions ?? []
  const observations = scenario.observations ?? []

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
        <span className="flex items-center gap-3 text-xs/5 text-gray-500 dark:text-gray-400">
          {findings > 0 && (
            <span className="text-amber-700 dark:text-amber-400">
              {findings} finding{findings === 1 ? '' : 's'}
            </span>
          )}
          <span>{formatDurationMs(scenario.duration_ms)}</span>
        </span>
      }
    >
      <div className="flex flex-col gap-5">
        {scenario.description && <p className="text-sm/6 text-gray-700 dark:text-gray-300">{scenario.description}</p>}
        {scenario.params && Object.keys(scenario.params).length > 0 && <ParamsList params={scenario.params} />}
        {scenario.error && (
          <div className="rounded-xs border border-red-200 bg-red-50 px-3 py-2 font-mono text-xs/5 break-all text-red-800 dark:border-red-800 dark:bg-red-900/20 dark:text-red-200">
            {scenario.error}
          </div>
        )}
        {metrics.length > 0 && (
          <Section title="Metrics">
            <MetricsGrid metrics={metrics} />
          </Section>
        )}
        {latencies.length > 0 && (
          <Section title="Latencies">
            <LatencyTable latencies={latencies} />
          </Section>
        )}
        <Section title="Pool timeline">
          <PoolTimelineChart timeline={scenario.timeline ?? []} events={scenario.events ?? []} />
        </Section>
        {assertions.length > 0 && (
          <Section title="Assertions">
            <AssertionsTable assertions={assertions} />
          </Section>
        )}
        {observations.length > 0 && (
          <Section title="Observations">
            <ObservationsTable observations={observations} />
          </Section>
        )}
      </div>
    </Card>
  )
}
