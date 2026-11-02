import type {
  IndexEntry,
  MempoolEvent,
  MempoolLatency,
  MempoolRunResult,
  MempoolScenarioResult,
  MempoolScenarioStatus,
} from '@/api/types'
import { formatBytes } from '@/utils/format'

export const MEMPOOL_MODE = 'mempool'

// isMempoolMode is true for a run, index entry or suite of the mempool
// executor. An absent mode means a replay run.
export function isMempoolMode(item: { mode?: string } | null | undefined): boolean {
  return item?.mode === MEMPOOL_MODE
}

const STATUS_RANK: Record<MempoolScenarioStatus, number> = { error: 0, failed: 1, passed: 2 }

// sortScenarios puts error and failed scenarios first. Inside a status it
// keeps the suite order of the result file.
export function sortScenarios(scenarios: MempoolScenarioResult[]): MempoolScenarioResult[] {
  return scenarios
    .map((s, i) => ({ s, i }))
    .sort((a, b) => (STATUS_RANK[a.s.status] ?? 3) - (STATUS_RANK[b.s.status] ?? 3) || a.i - b.i)
    .map(({ s }) => s)
}

// A finding is a soft assertion that did not pass. It does not fail a
// scenario. Observations (e.g. error texts) are not findings: they have no
// correct value and only matter when the UI compares them between clients.
export function scenarioFindingCount(scenario: MempoolScenarioResult): number {
  return (scenario.assertions ?? []).filter((a) => a.severity === 'soft' && !a.passed).length
}

export interface MempoolSummaryCounts {
  total: number
  passed: number
  failed: number
  error: number
  hardFailures: number
  findings: number
  observations: number
}

export function summarizeMempoolResult(result: MempoolRunResult): MempoolSummaryCounts {
  const scenarios = result.scenarios ?? []
  const counts: MempoolSummaryCounts = { total: scenarios.length, passed: 0, failed: 0, error: 0, hardFailures: 0, findings: 0, observations: 0 }
  for (const s of scenarios) {
    if (s.status === 'passed') counts.passed++
    else if (s.status === 'failed') counts.failed++
    else counts.error++
    counts.hardFailures += (s.assertions ?? []).filter((a) => a.severity === 'hard' && !a.passed).length
    counts.findings += scenarioFindingCount(s)
    counts.observations += (s.observations ?? []).length
  }
  return counts
}

// selectMempoolPeerRuns picks one run per client for the comparison: the
// current run for its own client and the latest completed mempool run of
// the same suite for every other client. Clients are sorted by name.
export function selectMempoolPeerRuns(entries: IndexEntry[], suiteHash: string | undefined, currentRunId: string): IndexEntry[] {
  if (!suiteHash) return []
  const byClient = new Map<string, IndexEntry>()
  const current = entries.find((e) => e.run_id === currentRunId)

  for (const e of entries) {
    if (e.suite_hash !== suiteHash || !isMempoolMode(e)) continue
    if (e.status === 'running' || e.deletion_requested_at) continue
    const prev = byClient.get(e.instance.client)
    if (!prev || e.timestamp > prev.timestamp) byClient.set(e.instance.client, e)
  }
  if (current) byClient.set(current.instance.client, current)

  return [...byClient.values()].sort((a, b) => a.instance.client.localeCompare(b.instance.client))
}

// A client result in the comparison. The key is the column name.
export interface ClientMempoolResult {
  client: string
  runId: string
  result: MempoolRunResult
}

export interface ComparisonRow {
  scenario: string
  key: string
  label: string
  unit: string
  better?: 'lower' | 'higher'
  values: Record<string, number | undefined>
  best: string[]
  worst: string[]
}

// scenarioOrder returns the union of the scenario names, in the order the
// clients list them (first client first).
function scenarioOrder(results: ClientMempoolResult[]): string[] {
  const names: string[] = []
  const seen = new Set<string>()
  for (const r of results) {
    for (const s of r.result.scenarios ?? []) {
      if (!seen.has(s.name)) {
        seen.add(s.name)
        names.push(s.name)
      }
    }
  }
  return names
}

function findScenario(r: ClientMempoolResult, name: string): MempoolScenarioResult | undefined {
  return (r.result.scenarios ?? []).find((s) => s.name === name)
}

// rankValues finds the best and worst clients of a row. It returns empty
// lists when the row has no direction, fewer than two values, or no spread.
export function rankValues(values: Record<string, number | undefined>, better?: 'lower' | 'higher'): { best: string[]; worst: string[] } {
  const defined = Object.entries(values).filter((e): e is [string, number] => typeof e[1] === 'number' && Number.isFinite(e[1]))
  if (!better || defined.length < 2) return { best: [], worst: [] }
  const nums = defined.map(([, v]) => v)
  const min = Math.min(...nums)
  const max = Math.max(...nums)
  if (min === max) return { best: [], worst: [] }
  const lo = defined.filter(([, v]) => v === min).map(([c]) => c)
  const hi = defined.filter(([, v]) => v === max).map(([c]) => c)
  return better === 'lower' ? { best: lo, worst: hi } : { best: hi, worst: lo }
}

// buildComparisonRows makes one row per metric key and two rows (p50 and
// p99) per latency key, for each scenario.
export function buildComparisonRows(results: ClientMempoolResult[]): ComparisonRow[] {
  const rows: ComparisonRow[] = []

  for (const scenario of scenarioOrder(results)) {
    const metricRows = new Map<string, ComparisonRow>()
    const latencyRows = new Map<string, ComparisonRow>()

    for (const r of results) {
      const s = findScenario(r, scenario)
      if (!s) continue

      for (const m of s.metrics ?? []) {
        let row = metricRows.get(m.key)
        if (!row) {
          row = { scenario, key: m.key, label: m.label, unit: m.unit, better: m.better, values: {}, best: [], worst: [] }
          metricRows.set(m.key, row)
        }
        row.values[r.client] = m.value
      }

      for (const l of s.latencies ?? []) {
        for (const q of ['p50', 'p99'] as const) {
          const key = `${l.key}.${q}`
          let row = latencyRows.get(key)
          if (!row) {
            row = { scenario, key, label: `${l.label} ${q}`, unit: l.unit, better: 'lower', values: {}, best: [], worst: [] }
            latencyRows.set(key, row)
          }
          row.values[r.client] = l.count > 0 ? l[q] : undefined
        }
      }
    }

    for (const row of [...metricRows.values(), ...latencyRows.values()]) {
      const { best, worst } = rankValues(row.values, row.better)
      row.best = best
      row.worst = worst
      rows.push(row)
    }
  }

  return rows
}

export interface ScenarioDifferences {
  scenario: string
  statuses: Record<string, MempoolScenarioStatus | undefined>
  assertions: { name: string; severity: 'hard' | 'soft'; values: Record<string, boolean | undefined> }[]
  observations: { key: string; label: string; values: Record<string, string | undefined> }[]
}

function distinctDefined<T>(values: Record<string, T | undefined>): number {
  return new Set(Object.values(values).filter((v) => v !== undefined)).size
}

// computeDifferences lists, per scenario, the assertions whose result
// differs between clients and the observations whose value differs.
// Scenarios without a difference are left out.
export function computeDifferences(results: ClientMempoolResult[]): ScenarioDifferences[] {
  if (results.length < 2) return []
  const out: ScenarioDifferences[] = []

  for (const scenario of scenarioOrder(results)) {
    const statuses: Record<string, MempoolScenarioStatus | undefined> = {}
    const assertions = new Map<string, ScenarioDifferences['assertions'][number]>()
    const observations = new Map<string, ScenarioDifferences['observations'][number]>()

    for (const r of results) {
      const s = findScenario(r, scenario)
      statuses[r.client] = s?.status
      if (!s) continue
      for (const a of s.assertions ?? []) {
        let row = assertions.get(a.name)
        if (!row) {
          row = { name: a.name, severity: a.severity, values: {} }
          assertions.set(a.name, row)
        }
        row.values[r.client] = a.passed
      }
      for (const o of s.observations ?? []) {
        let row = observations.get(o.key)
        if (!row) {
          row = { key: o.key, label: o.label, values: {} }
          observations.set(o.key, row)
        }
        row.values[r.client] = o.value
      }
    }

    const diff: ScenarioDifferences = {
      scenario,
      statuses,
      assertions: [...assertions.values()].filter((a) => distinctDefined(a.values) > 1),
      observations: [...observations.values()].filter((o) => distinctDefined(o.values) > 1),
    }
    if (diff.assertions.length > 0 || diff.observations.length > 0) out.push(diff)
  }

  return out
}

// histogramBuckets returns the per-bucket counts of a latency histogram.
// The backend may write cumulative ("le") counts, as Prometheus does. The
// counts are cumulative when they never decrease and the last bucket holds
// the total count, but the plain sum does not.
export function histogramBuckets(latency: Pick<MempoolLatency, 'count' | 'histogram'>): { le: number; count: number }[] {
  const h = latency.histogram ?? []
  if (h.length === 0) return []
  const sum = h.reduce((acc, b) => acc + b.count, 0)
  const monotonic = h.every((b, i) => i === 0 || b.count >= h[i - 1].count)
  const cumulative = h.length > 1 && monotonic && h[h.length - 1].count === latency.count && sum !== latency.count
  if (!cumulative) return h
  return h.map((b, i) => ({ le: b.le, count: i === 0 ? b.count : b.count - h[i - 1].count }))
}

// splitEventMarkers keeps the events to draw as vertical lines. A kind with
// more than maxPerKind events (e.g. one "send" per transaction) would hide
// the chart, so it goes to the collapsed list with its count.
export function splitEventMarkers(events: MempoolEvent[], maxPerKind = 20): { markers: MempoolEvent[]; collapsed: { kind: string; count: number }[] } {
  const counts = new Map<string, number>()
  for (const e of events ?? []) counts.set(e.kind, (counts.get(e.kind) ?? 0) + 1)
  const markers = (events ?? []).filter((e) => (counts.get(e.kind) ?? 0) <= maxPerKind)
  const collapsed = [...counts.entries()]
    .filter(([, c]) => c > maxPerKind)
    .map(([kind, count]) => ({ kind, count }))
  return { markers, collapsed }
}

// formatMempoolValue formats a metric value with its unit.
export function formatMempoolValue(value: number | undefined, unit: string): string {
  if (value === undefined || value === null || !Number.isFinite(value)) return '-'
  if (unit === 'bytes') return formatBytes(value)
  if (unit === 'ms') return value >= 1000 ? `${(value / 1000).toFixed(2)} s` : `${value.toFixed(value < 10 ? 2 : 1)} ms`
  const num = new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(value)
  if (!unit || unit === 'count') return num
  if (unit === '%') return `${num}%`
  return `${num} ${unit}`
}

// formatParam renders a scenario param value as short text.
export function formatParam(value: unknown): string {
  if (value === null || value === undefined) return '-'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}
