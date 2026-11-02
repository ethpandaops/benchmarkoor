import type {
  MempoolCheckExplanation,
  IndexEntry,
  MempoolAssertion,
  MempoolEvent,
  MempoolLatency,
  MempoolMetric,
  MempoolRunResult,
  MempoolScenarioResult,
  MempoolScenarioStatus,
} from '@/api/types'

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

// ---------------------------------------------------------------------------
// Checks (assertions)

export type CheckState = 'hard-fail' | 'soft-fail' | 'passed'

export function checkState(a: Pick<MempoolAssertion, 'severity' | 'passed'>): CheckState {
  if (a.passed) return 'passed'
  return a.severity === 'hard' ? 'hard-fail' : 'soft-fail'
}

const CHECK_RANK: Record<CheckState, number> = { 'hard-fail': 0, 'soft-fail': 1, passed: 2 }

// orderChecks puts hard failures first, then soft failures (findings), then
// passes. Inside a state it keeps the order of the result file.
export function orderChecks<T extends Pick<MempoolAssertion, 'severity' | 'passed'>>(assertions: T[]): T[] {
  return assertions
    .map((a, i) => ({ a, i }))
    .sort((x, y) => CHECK_RANK[checkState(x.a)] - CHECK_RANK[checkState(y.a)] || x.i - y.i)
    .map(({ a }) => a)
}

export interface CheckCounts {
  hard: { passed: number; total: number }
  soft: { passed: number; total: number }
}

export function countChecks(assertions: Pick<MempoolAssertion, 'severity' | 'passed'>[]): CheckCounts {
  const counts: CheckCounts = { hard: { passed: 0, total: 0 }, soft: { passed: 0, total: 0 } }
  for (const a of assertions) {
    const c = a.severity === 'hard' ? counts.hard : counts.soft
    c.total++
    if (a.passed) c.passed++
  }
  return counts
}

// ---------------------------------------------------------------------------
// Groups

const GROUP_LABELS: Record<string, string> = {
  load: 'Load',
  engine: 'Engine API',
  fill: 'Fill',
  evict: 'Eviction',
  hog: 'One sender',
  fall: 'Base fee fall',
  payload: 'Payload',
  promotion: 'Promotion',
  build: 'Build',
  blobs: 'Blobs',
  new_payload: 'newPayload',
  fcu: 'forkchoiceUpdated',
  settle: 'Pool settle',
  legacy: 'Legacy',
  'access-list': 'Access list',
  'dynamic-fee': 'Dynamic fee',
  'set-code': 'Set code',
  blob: 'Blob',
  rejection: 'Rejection',
  replacement: 'Replacement',
  reorg: 'Reorg',
  empty: 'Empty pool',
  full: 'Full pool',
}

// humanizeGroup turns an internal metric or latency group key into a
// heading. Unknown keys get spaces and a capital first letter.
export function humanizeGroup(group: string | undefined): string {
  if (!group) return ''
  const known = GROUP_LABELS[group]
  if (known) return known
  const blobs = /^blobs?(\d+)$/.exec(group)
  if (blobs) return `Blob ×${blobs[1]}`
  const text = group.replace(/[-_]+/g, ' ').trim()
  return text.charAt(0).toUpperCase() + text.slice(1)
}

export interface MetricGroup {
  key: string
  heading: string
  // detail is the label suffix that all metrics of the group share (e.g.
  // "Access list (2930)"). The UI strips it from the labels.
  detail?: string
  items: { metric: MempoolMetric; label: string }[]
}

// groupMetrics splits metrics by group, in the order of first appearance.
// With more than one group, a ": suffix" that every label of a group shares
// moves from the labels to the group.
export function groupMetrics(metrics: MempoolMetric[]): MetricGroup[] {
  const byKey = new Map<string, MempoolMetric[]>()
  for (const m of metrics) {
    const g = m.group ?? ''
    byKey.set(g, [...(byKey.get(g) ?? []), m])
  }
  const multi = byKey.size > 1
  return [...byKey.entries()].map(([key, items]) => {
    const suffixes = items.map((m) => {
      const i = m.label.lastIndexOf(': ')
      return i > 0 ? m.label.slice(i + 2) : undefined
    })
    const shared = multi && suffixes[0] !== undefined && suffixes.every((s) => s === suffixes[0]) ? suffixes[0] : undefined
    return {
      key,
      heading: multi ? humanizeGroup(key) || 'Other' : '',
      detail: shared,
      items: items.map((m) => ({
        metric: m,
        label: shared ? m.label.slice(0, m.label.length - shared.length - 2) : m.label,
      })),
    }
  })
}

// ---------------------------------------------------------------------------
// Number formatting

const numberFormats = new Map<string, Intl.NumberFormat>()

function numberFormat(options: Intl.NumberFormatOptions): Intl.NumberFormat {
  const key = JSON.stringify(options)
  let f = numberFormats.get(key)
  if (!f) {
    f = new Intl.NumberFormat('en-US', options)
    numberFormats.set(key, f)
  }
  return f
}

// formatSig formats a number with `digits` significant digits and thousands
// separators. It never rounds the integer part: 13268.3 shows as 13,268.
// With pad, it keeps trailing zeros (2 shows as 2.00), so a column of
// durations has the same precision on every row.
export function formatSig(value: number, digits = 3, pad = false): string {
  if (!Number.isFinite(value)) return '-'
  if (value === 0) return '0'
  const abs = Math.abs(value)
  if (abs < 1) {
    return numberFormat(pad ? { minimumSignificantDigits: digits, maximumSignificantDigits: digits } : { maximumSignificantDigits: digits }).format(value)
  }
  const intDigits = Math.floor(Math.log10(abs)) + 1
  const fraction = Math.max(0, digits - intDigits)
  return numberFormat(pad ? { minimumFractionDigits: fraction, maximumFractionDigits: fraction } : { maximumFractionDigits: fraction }).format(value)
}

export type DurationUnit = 'µs' | 'ms' | 's'

// durationUnitFor picks the unit that keeps a millisecond value between 1
// and 1000.
export function durationUnitFor(ms: number): DurationUnit {
  const abs = Math.abs(ms)
  if (abs > 0 && abs < 1) return 'µs'
  if (abs >= 1000) return 's'
  return 'ms'
}

export function formatDuration(ms: number, unit: DurationUnit = durationUnitFor(ms)): string {
  if (!Number.isFinite(ms)) return '-'
  const v = unit === 'µs' ? ms * 1000 : unit === 's' ? ms / 1000 : ms
  return `${formatSig(v, 3, true)} ${unit}`
}

const BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB'] as const
type ByteUnit = (typeof BYTE_UNITS)[number]

function byteUnitFor(bytes: number): ByteUnit {
  let i = 0
  let v = Math.abs(bytes)
  while (v >= 1024 && i < BYTE_UNITS.length - 1) {
    v /= 1024
    i++
  }
  return BYTE_UNITS[i]
}

function formatBytesIn(bytes: number, unit: ByteUnit = byteUnitFor(bytes)): string {
  return `${formatSig(bytes / 1024 ** BYTE_UNITS.indexOf(unit))} ${unit}`
}

// formatMempoolValue formats a metric value with its unit. Durations ("ms")
// and sizes ("bytes") pick a readable unit per value.
export function formatMempoolValue(value: number | undefined | null, unit: string): string {
  if (value === undefined || value === null || !Number.isFinite(value)) return '-'
  return valueFormatter(unit, [value])(value)
}

// valueFormatter returns a formatter that uses one display unit for all the
// given values (the unit of the largest), so a row or column reads evenly.
export function valueFormatter(unit: string, values: (number | undefined)[]): (v: number | undefined) => string {
  const nums = values.filter((v): v is number => typeof v === 'number' && Number.isFinite(v))
  const maxAbs = nums.length > 0 ? Math.max(...nums.map(Math.abs)) : 0
  return (v) => {
    if (v === undefined || v === null || !Number.isFinite(v)) return '-'
    if (unit === 'ms') return formatDuration(v, durationUnitFor(maxAbs))
    if (unit === 'bytes') return formatBytesIn(v, byteUnitFor(maxAbs))
    const num = formatSig(v)
    if (!unit || unit === 'count') return num
    if (unit === '%') return `${num}%`
    return `${num} ${unit}`
  }
}

// splitValueUnit splits a formatted value into number and unit text, for a
// large number with a smaller unit next to it.
export function splitValueUnit(text: string): { value: string; unit: string } {
  if (text.endsWith('%')) return { value: text.slice(0, -1), unit: '%' }
  const i = text.indexOf(' ')
  return i < 0 ? { value: text, unit: '' } : { value: text.slice(0, i), unit: text.slice(i + 1) }
}

// ---------------------------------------------------------------------------
// Client comparison

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

// currentFirst puts the column of the current run first and keeps the
// order of the others.
export function currentFirst<T extends { runId: string }>(items: T[], currentRunId: string): T[] {
  return [...items.filter((i) => i.runId === currentRunId), ...items.filter((i) => i.runId !== currentRunId)]
}

export interface ComparisonRow {
  scenario: string
  key: string
  kind: 'metric' | 'latency'
  label: string
  unit: string
  better?: 'lower' | 'higher'
  values: Record<string, number | undefined>
  best: string[]
  worst: string[]
}

// scenarioOrder returns the union of the scenario names, in the order the
// clients list them (first client first).
export function scenarioOrder(results: ClientMempoolResult[]): string[] {
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

export function findScenario(r: ClientMempoolResult, name: string): MempoolScenarioResult | undefined {
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
          row = { scenario, key: m.key, kind: 'metric', label: m.label, unit: m.unit, better: m.better, values: {}, best: [], worst: [] }
          metricRows.set(m.key, row)
        }
        row.values[r.client] = m.value
      }

      for (const l of s.latencies ?? []) {
        for (const q of ['p50', 'p99'] as const) {
          const key = `${l.key}.${q}`
          let row = latencyRows.get(key)
          if (!row) {
            row = { scenario, key, kind: 'latency', label: `${l.label} ${q}`, unit: l.unit, better: 'lower', values: {}, best: [], worst: [] }
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

// barFraction is the bar length of a value relative to the largest absolute
// value of its row, from 0 to 1.
export function barFraction(value: number | undefined, values: (number | undefined)[]): number {
  if (value === undefined || !Number.isFinite(value)) return 0
  const max = Math.max(0, ...values.filter((v): v is number => typeof v === 'number' && Number.isFinite(v)).map(Math.abs))
  return max > 0 ? Math.abs(value) / max : 0
}

// relativeToBest describes how far a value is from the best value of its
// row, e.g. "2.1× slower" or "48% lower". It returns undefined for the best
// value itself or without a direction. When a value is zero or negative,
// ratios mean nothing, so it gives the difference instead.
export function relativeToBest(
  value: number | undefined,
  best: number | undefined,
  better: 'lower' | 'higher' | undefined,
  unit: string,
  format: (v: number) => string = (v) => formatMempoolValue(v, unit),
): string | undefined {
  if (!better || value === undefined || best === undefined || !Number.isFinite(value) || !Number.isFinite(best)) return undefined
  if (value === best) return undefined
  const word = better === 'higher' ? 'lower' : unit === 'ms' ? 'slower' : 'higher'
  if (value <= 0 || best <= 0) return `${format(Math.abs(value - best))} ${word}`
  const ratio = better === 'lower' ? value / best : best / value
  if (ratio >= 2) return `${formatSig(ratio, 2)}× ${word}`
  const pct = better === 'lower' ? (value / best - 1) * 100 : (1 - value / best) * 100
  return `${formatSig(pct, 2)}% ${word}`
}

// ---------------------------------------------------------------------------
// Differences

// ObservationOutcome classifies an observation value. "accepted" and
// "rejected" are the outcome of a tx the scenario sent; "value" is a number
// or a count text (e.g. "6", "256 kept, 0 rejected"), compared as raw text.
export type ObservationOutcome = 'accepted' | 'rejected' | 'value'

export function classifyObservation(value: string | undefined): ObservationOutcome | undefined {
  if (value === undefined) return undefined
  const v = value.trim()
  if (v === '') return 'value'
  if (/^accepted\b/i.test(v)) return 'accepted'
  if (/^[-+]?\d/.test(v)) return 'value'
  return 'rejected'
}

// OUTCOME_CLASS colors the outcome chip of an observation.
export const OUTCOME_CLASS: Record<Exclude<ObservationOutcome, 'value'>, string> = {
  accepted: 'bg-blue-50 text-blue-700 ring-1 ring-blue-200 dark:bg-blue-900/30 dark:text-blue-300 dark:ring-blue-800/60',
  rejected: 'bg-gray-100 text-gray-700 ring-1 ring-gray-200 dark:bg-gray-700 dark:text-gray-300 dark:ring-gray-600',
}

export interface AssertionDifference {
  name: string
  severity: 'hard' | 'soft'
  values: Record<string, boolean | undefined>
  explain?: MempoolCheckExplanation
}

export interface ObservationDifference {
  key: string
  label: string
  values: Record<string, string | undefined>
  outcomes: Record<string, ObservationOutcome | undefined>
  explain?: MempoolCheckExplanation
}

export interface ScenarioDifferences {
  scenario: string
  statuses: Record<string, MempoolScenarioStatus | undefined>
  assertions: AssertionDifference[]
  // Observations whose outcome differs, or numeric values that differ.
  observations: ObservationDifference[]
  // Observations with the same outcome everywhere but a different text
  // (e.g. all rejected, each with its own error message).
  wordingOnly: ObservationDifference[]
}

function distinctDefined<T>(values: Record<string, T | undefined>): number {
  return new Set(Object.values(values).filter((v) => v !== undefined)).size
}

// computeDifferences lists, per scenario, the assertions whose result
// differs between clients and the observations whose outcome or value
// differs. Scenarios without a difference are left out.
export function computeDifferences(results: ClientMempoolResult[]): ScenarioDifferences[] {
  if (results.length < 2) return []
  const out: ScenarioDifferences[] = []

  for (const scenario of scenarioOrder(results)) {
    const statuses: Record<string, MempoolScenarioStatus | undefined> = {}
    const assertions = new Map<string, AssertionDifference>()
    const observations = new Map<string, ObservationDifference>()

    for (const r of results) {
      const s = findScenario(r, scenario)
      statuses[r.client] = s?.status
      if (!s) continue
      for (const a of s.assertions ?? []) {
        let row = assertions.get(a.name)
        if (!row) {
          row = { name: a.name, severity: a.severity, values: {}, explain: a.explain }
          assertions.set(a.name, row)
        }
        row.values[r.client] = a.passed
      }
      for (const o of s.observations ?? []) {
        let row = observations.get(o.key)
        if (!row) {
          row = { key: o.key, label: o.label, values: {}, outcomes: {}, explain: o.explain }
          observations.set(o.key, row)
        }
        row.values[r.client] = o.value
        row.outcomes[r.client] = classifyObservation(o.value)
      }
    }

    const diff: ScenarioDifferences = {
      scenario,
      statuses,
      assertions: [...assertions.values()].filter((a) => distinctDefined(a.values) > 1),
      observations: [],
      wordingOnly: [],
    }
    for (const o of observations.values()) {
      if (distinctDefined(o.values) <= 1) continue
      const outcomes = new Set(Object.values(o.outcomes).filter((v) => v !== undefined))
      if (outcomes.size > 1 || outcomes.has('value')) diff.observations.push(o)
      else diff.wordingOnly.push(o)
    }
    if (diff.assertions.length > 0 || diff.observations.length > 0 || diff.wordingOnly.length > 0) out.push(diff)
  }

  return out
}

// ---------------------------------------------------------------------------
// Latencies and events

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

// The fixed order of the event lanes. Unknown kinds follow, in the order
// they first appear.
export const EVENT_KIND_ORDER = ['send', 'pending', 'settled', 'reorg', 'basefee', 'fcu', 'newPayload', 'getPayload']

// groupEventsByKind splits the events into one lane per kind. No event is
// dropped: a kind with many events (e.g. one "send" per tx) still gets
// thin ticks.
export function groupEventsByKind(events: MempoolEvent[] | undefined): { kind: string; events: MempoolEvent[] }[] {
  const byKind = new Map<string, MempoolEvent[]>()
  for (const e of events ?? []) byKind.set(e.kind, [...(byKind.get(e.kind) ?? []), e])
  const rank = (k: string) => {
    const i = EVENT_KIND_ORDER.indexOf(k)
    return i < 0 ? EVENT_KIND_ORDER.length : i
  }
  const kinds = [...byKind.keys()]
  return kinds
    .map((kind, i) => ({ kind, i }))
    .sort((a, b) => rank(a.kind) - rank(b.kind) || a.i - b.i)
    .map(({ kind }) => ({ kind, events: byKind.get(kind)! }))
}

// formatParam renders a scenario param value as short text.
export function formatParam(value: unknown): string {
  if (value === null || value === undefined) return '-'
  if (typeof value === 'object') return JSON.stringify(value)
  return String(value)
}

// SEVERITY_HELP explains the two check severities. The UI shows it as a
// tooltip wherever a severity appears.
export const SEVERITY_HELP: Record<'hard' | 'soft', string> = {
  hard:
    'Hard check: a rule a client must never break (e.g. an invalid tx in a payload, an included tx ' +
    'left in the pool). A failed hard check fails the scenario.',
  soft:
    'Soft check: pool policy that clients may choose differently (e.g. the price bump a replacement ' +
    'needs). A failed soft check is a finding to look at, not a failure; the scenario still passes.',
}

// EVENT_HELP explains each event kind of the pool timeline. The UI shows it
// as a tooltip on the event legend.
export const EVENT_HELP: Record<string, string> = {
  send: 'The harness started to send a batch of txs (eth_sendRawTransaction). The detail says which batch.',
  pending:
    'A "time until pending" measurement ended: the txs were timed from the send until the pool listed them as pending.',
  fcu:
    'engine_forkchoiceUpdated: the harness set a new head. With build attributes it also starts a payload build.',
  getPayload: 'engine_getPayload: the harness took a built payload from the client (block number, txs, gas).',
  newPayload: 'engine_newPayload: the harness sent a block to the client to import.',
  reorg: 'The head moved to a sibling block on a different branch (a reorg).',
  settled:
    'After a head change, the pool reached its expected state (included txs gone, reinjected txs back).',
  basefee: 'The base fee crossed the fee cap of the test txs (up, then down again).',
  withdrawal: 'A block credited a withdrawal to an account (a balance change without a tx).',
  drain:
    'The harness stopped clearing the pool: the txs still pending are ones the builder does not include (unpayable or priced out).',
}

// eventHelp returns the explanation of an event kind, or a generic one.
export function eventHelp(kind: string): string {
  return EVENT_HELP[kind] ?? `A "${kind}" action of the harness.`
}

// ExplainKind picks the labels of an explanation: a check tests something,
// an observation records a value, a metric (or latency) measures one.
export type ExplainKind = 'check' | 'observation' | 'metric'

export const EXPLAIN_LABELS: Record<ExplainKind, [string, string, string]> = {
  check: ['What it tests', 'Why it matters', 'OK results'],
  observation: ['What it records', 'Why it matters', 'How to read it'],
  metric: ['What it measures', 'Why it matters', 'How to read it'],
}

// explainText is an explanation as plain text, for a tooltip. A boolean
// kind is the older form: true means an observation.
export function explainText(e: MempoolCheckExplanation | undefined, kind: ExplainKind | boolean = 'check'): string | undefined {
  if (!e) return undefined
  const k: ExplainKind = kind === true ? 'observation' : kind === false ? 'check' : kind
  const [what, why, ok] = EXPLAIN_LABELS[k]
  return `${what}: ${e.what}\n\n${why}: ${e.why}\n\n${ok}: ${e.ok}`
}

// ---------------------------------------------------------------------------
// Blocks and fees

// feeUnit picks gwei for fees of 0.01 gwei and more, else wei, so a blob
// base fee of a few wei does not show as "0.000000001".
export function feeUnit(maxWei: number): { unit: 'gwei' | 'wei'; scale: number } {
  return maxWei >= 1e7 ? { unit: 'gwei', scale: 1e9 } : { unit: 'wei', scale: 1 }
}

// blockAt returns the last block imported at or before t_ms. blocks must be
// sorted by t_ms.
export function blockAt<T extends { t_ms: number }>(blocks: T[] | undefined, tMs: number): T | undefined {
  if (!blocks || blocks.length === 0 || blocks[0].t_ms > tMs) return undefined
  let lo = 0
  let hi = blocks.length - 1
  while (lo < hi) {
    const mid = (lo + hi + 1) >> 1
    if (blocks[mid].t_ms <= tMs) lo = mid
    else hi = mid - 1
  }
  return blocks[lo]
}
