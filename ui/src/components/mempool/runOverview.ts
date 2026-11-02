import type { MempoolAssertion, MempoolCheckExplanation, MempoolObservation, MempoolScenarioResult } from '@/api/types'
import { isInvariant } from './behaviours'
import { formatMempoolValue } from './mempoolUtils'

// scenarioDomId is the stable element id of a scenario card, for links.
export function scenarioDomId(name: string): string {
  return `scenario-${name.replace(/[^a-zA-Z0-9_-]+/g, '-')}`
}

// ---------------------------------------------------------------------------
// Problems

export interface RunProblem {
  scenario: string
  kind: 'hard-fail' | 'error'
  // The check name, or "Scenario error".
  name: string
  actual?: string
  expected?: string
}

// collectProblems lists every failed hard check and every scenario error, in
// the order of the scenarios.
export function collectProblems(scenarios: MempoolScenarioResult[]): RunProblem[] {
  const out: RunProblem[] = []
  for (const s of scenarios) {
    if (s.status === 'error' || s.error) {
      out.push({ scenario: s.name, kind: 'error', name: 'Scenario error', actual: s.error })
    }
    for (const a of s.assertions ?? []) {
      if (a.severity === 'hard' && !a.passed) {
        out.push({ scenario: s.name, kind: 'hard-fail', name: a.name, actual: a.actual, expected: a.expected })
      }
    }
  }
  return out
}

// ---------------------------------------------------------------------------
// Invariants

export interface InvariantSummary {
  name: string
  severity: 'hard' | 'soft'
  explain?: MempoolCheckExplanation
  // Scenarios that checked the invariant, in scenario order.
  scenarios: string[]
  passed: number
  failed: number
  failures: { scenario: string; actual?: string; expected?: string }[]
}

// summarizeInvariants makes one entry per distinct invariant name. Failed
// invariants come first; inside a state the order is the first appearance.
export function summarizeInvariants(scenarios: MempoolScenarioResult[]): InvariantSummary[] {
  const byName = new Map<string, InvariantSummary>()
  for (const s of scenarios) {
    for (const a of s.assertions ?? []) {
      if (!isInvariant(a)) continue
      let inv = byName.get(a.name)
      if (!inv) {
        inv = { name: a.name, severity: a.severity, explain: a.explain, scenarios: [], passed: 0, failed: 0, failures: [] }
        byName.set(a.name, inv)
      }
      inv.explain ??= a.explain
      if (!inv.scenarios.includes(s.name)) inv.scenarios.push(s.name)
      if (a.passed) inv.passed++
      else {
        inv.failed++
        inv.failures.push({ scenario: s.name, actual: a.actual, expected: a.expected })
      }
    }
  }
  return [...byName.values()].map((inv, i) => ({ inv, i })).sort((a, b) => Number(b.inv.failed > 0) - Number(a.inv.failed > 0) || a.i - b.i).map(({ inv }) => inv)
}

// ---------------------------------------------------------------------------
// Run page filter

export type ScenarioFilter = 'all' | 'problems'

// isProblemCheck is true for a failed check, hard or soft.
export function isProblemCheck(a: Pick<MempoolAssertion, 'passed'>): boolean {
  return !a.passed
}

// scenarioIsPassing is true for a passed scenario with no failed check.
export function scenarioIsPassing(s: MempoolScenarioResult): boolean {
  return s.status === 'passed' && !s.error && (s.assertions ?? []).every((a) => a.passed)
}

export interface FilteredScenario {
  scenario: MempoolScenarioResult
  assertions: MempoolAssertion[]
  observations: MempoolObservation[]
}

// filterScenarios applies the run page filter. With "problems" a scenario
// keeps only its failed checks and no observations, and a scenario with no
// failed check and no error goes away. hidePassing drops the passing
// scenarios.
export function filterScenarios(scenarios: MempoolScenarioResult[], filter: ScenarioFilter, hidePassing: boolean): FilteredScenario[] {
  const out: FilteredScenario[] = []
  for (const s of scenarios) {
    if (hidePassing && scenarioIsPassing(s)) continue
    if (filter === 'problems') {
      const failing = (s.assertions ?? []).filter(isProblemCheck)
      if (failing.length === 0 && !s.error && s.status !== 'error') continue
      out.push({ scenario: s, assertions: failing, observations: [] })
    } else {
      out.push({ scenario: s, assertions: s.assertions ?? [], observations: s.observations ?? [] })
    }
  }
  return out
}

// ---------------------------------------------------------------------------
// Headline numbers

type HeadlineSource = { kind: 'metric'; key: string } | { kind: 'latency'; key: string; q: 'p50' | 'p99' }

// SCENARIO_HEADLINES picks the 1 or 2 values that a collapsed scenario card
// shows in its header.
export const SCENARIO_HEADLINES: Record<string, HeadlineSource[]> = {
  admission: [{ kind: 'metric', key: 'concurrent_tps.dynamic-fee' }],
  'admission-rejections': [{ kind: 'latency', key: 'reject_latency', q: 'p50' }],
  'nonce-gap-promotion': [{ kind: 'latency', key: 'promotion', q: 'p50' }],
  replacement: [{ kind: 'latency', key: 'replace_visible', q: 'p50' }],
  'payload-ordering': [{ kind: 'metric', key: 'fill_ratio' }],
  'import-full-pool': [{ kind: 'metric', key: 'settle.overhead' }],
  'reorg-reinjection': [{ kind: 'latency', key: 'reorg_reinjected', q: 'p50' }],
  'eviction-limits': [{ kind: 'metric', key: 'high.kept' }],
  'base-fee-movement': [{ kind: 'metric', key: 'blocks_to_include' }],
  'payload-latency': [{ kind: 'latency', key: 'staleness', q: 'p50' }],
  'sustained-load': [
    { kind: 'latency', key: 'inclusion', q: 'p50' },
    { kind: 'metric', key: 'rate' },
  ],
  'setcode-delegation': [{ kind: 'metric', key: 'inflight.delegated' }],
  'blob-pool': [{ kind: 'metric', key: 'reorg.reinjected' }],
  'balance-changes': [{ kind: 'metric', key: 'drain.dropped' }],
}

export interface Headline {
  label: string
  text: string
}

// scenarioHeadlines returns the header values of a scenario that it has.
export function scenarioHeadlines(s: MempoolScenarioResult): Headline[] {
  const out: Headline[] = []
  for (const src of SCENARIO_HEADLINES[s.name] ?? []) {
    if (src.kind === 'metric') {
      const m = (s.metrics ?? []).find((x) => x.key === src.key)
      if (m) out.push({ label: m.label, text: formatMempoolValue(m.value, m.unit) })
    } else {
      const l = (s.latencies ?? []).find((x) => x.key === src.key)
      if (l && l.count > 0) out.push({ label: `${l.label} (${src.q})`, text: formatMempoolValue(l[src.q], 'ms') })
    }
  }
  return out
}

// ---------------------------------------------------------------------------
// Navigation to a scenario card

export const OPEN_SCENARIO_EVENT = 'mempool:open-scenario'

// openScenario asks the card of a scenario to open and scroll into view.
export function openScenario(name: string): void {
  window.dispatchEvent(new CustomEvent<string>(OPEN_SCENARIO_EVENT, { detail: name }))
}
