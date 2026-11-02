import type { MempoolAssertion, MempoolCheckExplanation, MempoolScenarioResult } from '@/api/types'
import { checkState, classifyObservation, scenarioOrder, type CheckState, type ClientMempoolResult, type ObservationOutcome } from './mempoolUtils'

// Checks whose name starts with this prefix are invariants: rules the
// harness checks after every build and import, so they repeat in many
// scenarios.
export const INVARIANT_PREFIX = 'Invariant: '

export function isInvariant(a: Pick<MempoolAssertion, 'name'>): boolean {
  return a.name.startsWith(INVARIANT_PREFIX)
}

// ---------------------------------------------------------------------------
// Themes

export interface BehaviourTheme {
  key: string
  label: string
  scenarios: string[]
}

// BEHAVIOUR_THEMES groups the scenarios by the pool behaviour they test.
// A scenario that is not in the list goes to "Other".
export const BEHAVIOUR_THEMES: BehaviourTheme[] = [
  { key: 'admission', label: 'Admission', scenarios: ['admission', 'admission-rejections'] },
  { key: 'nonces', label: 'Nonces', scenarios: ['nonce-gap-promotion'] },
  { key: 'replacement', label: 'Replacement', scenarios: ['replacement'] },
  { key: 'ordering', label: 'Ordering & building', scenarios: ['payload-ordering', 'payload-latency'] },
  { key: 'import', label: 'Import & reorgs', scenarios: ['import-full-pool', 'reorg-reinjection'] },
  { key: 'limits', label: 'Limits & eviction', scenarios: ['eviction-limits'] },
  { key: 'fees', label: 'Fees', scenarios: ['base-fee-movement'] },
  { key: 'setcode', label: 'EIP-7702', scenarios: ['setcode-delegation'] },
  { key: 'blobs', label: 'Blobs', scenarios: ['blob-pool'] },
  { key: 'balance', label: 'Balance changes', scenarios: ['balance-changes'] },
  { key: 'load', label: 'Load', scenarios: ['sustained-load'] },
]

const OTHER_THEME: BehaviourTheme = { key: 'other', label: 'Other', scenarios: [] }

export function themeOf(scenario: string): BehaviourTheme {
  return BEHAVIOUR_THEMES.find((t) => t.scenarios.includes(scenario)) ?? OTHER_THEME
}

function themeRank(key: string): number {
  const i = BEHAVIOUR_THEMES.findIndex((t) => t.key === key)
  return i < 0 ? BEHAVIOUR_THEMES.length : i
}

// ---------------------------------------------------------------------------
// Rows

export type BehaviourCell =
  | { kind: 'check'; state: CheckState; expected?: string; actual?: string }
  | { kind: 'observation'; value: string; outcome: ObservationOutcome }

export interface BehaviourRow {
  // id is unique over the matrix: kind, scenario and check name or key.
  id: string
  scenario: string
  theme: string
  kind: 'check' | 'observation'
  label: string
  // key is the observation key; for a check it is the name.
  key: string
  severity?: 'hard' | 'soft'
  explain?: MempoolCheckExplanation
  cells: Record<string, BehaviourCell | undefined>
  // differs is true when the clients that have the row do not agree.
  differs: boolean
  // problem is true when a client has a failed check (hard or soft).
  problem: boolean
}

// cellsDiffer compares the cells of a row. Checks differ by state.
// Observations differ by outcome; numbers and counts ("value") also differ
// by text. Different error texts with the same outcome (each client has its
// own wording) do not count. A client without the row does not count.
export function cellsDiffer(cells: (BehaviourCell | undefined)[]): boolean {
  const defined = cells.filter((c): c is BehaviourCell => c !== undefined)
  if (defined.length < 2) return false
  if (defined[0].kind === 'check') {
    return new Set(defined.map((c) => (c.kind === 'check' ? c.state : ''))).size > 1
  }
  const obs = defined.filter((c): c is Extract<BehaviourCell, { kind: 'observation' }> => c.kind === 'observation')
  const outcomes = new Set(obs.map((c) => c.outcome))
  if (outcomes.size > 1) return true
  if (outcomes.has('value')) return new Set(obs.map((c) => c.value.trim())).size > 1
  return false
}

function scenarioOf(r: ClientMempoolResult, name: string): MempoolScenarioResult | undefined {
  return (r.result.scenarios ?? []).find((s) => s.name === name)
}

// buildBehaviourRows makes one row per non-invariant check and one row per
// observation, over the union of the clients. Rows follow the theme order,
// then the scenario order of the first client, then the order of the result
// file (checks before observations).
export function buildBehaviourRows(results: ClientMempoolResult[]): BehaviourRow[] {
  const scenarios = scenarioOrder(results)
    .map((name, i) => ({ name, i, theme: themeOf(name).key }))
    .sort((a, b) => themeRank(a.theme) - themeRank(b.theme) || a.i - b.i)

  const rows: BehaviourRow[] = []
  for (const { name: scenario, theme } of scenarios) {
    const checks = new Map<string, BehaviourRow>()
    const observations = new Map<string, BehaviourRow>()
    for (const r of results) {
      const s = scenarioOf(r, scenario)
      if (!s) continue
      for (const a of s.assertions ?? []) {
        if (isInvariant(a)) continue
        let row = checks.get(a.name)
        if (!row) {
          row = { id: `check:${scenario}:${a.name}`, scenario, theme, kind: 'check', label: a.name, key: a.name, severity: a.severity, explain: a.explain, cells: {}, differs: false, problem: false }
          checks.set(a.name, row)
        }
        row.explain ??= a.explain
        row.cells[r.client] = { kind: 'check', state: checkState(a), expected: a.expected, actual: a.actual }
      }
      for (const o of s.observations ?? []) {
        let row = observations.get(o.key)
        if (!row) {
          row = { id: `obs:${scenario}:${o.key}`, scenario, theme, kind: 'observation', label: o.label, key: o.key, explain: o.explain, cells: {}, differs: false, problem: false }
          observations.set(o.key, row)
        }
        row.explain ??= o.explain
        row.cells[r.client] = { kind: 'observation', value: o.value, outcome: classifyObservation(o.value) ?? 'value' }
      }
    }
    for (const row of [...checks.values(), ...observations.values()]) {
      const cells = results.map((r) => row.cells[r.client])
      row.differs = cellsDiffer(cells)
      row.problem = cells.some((c) => c?.kind === 'check' && c.state !== 'passed')
      rows.push(row)
    }
  }
  return rows
}

export type BehaviourFilter = 'all' | 'differences' | 'problems'

// filterBehaviourRows keeps the rows that match the filter and whose label,
// key or scenario contains every word of the query (case-insensitive).
export function filterBehaviourRows(rows: BehaviourRow[], filter: BehaviourFilter, query: string): BehaviourRow[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean)
  return rows.filter((row) => {
    if (filter === 'differences' && !row.differs) return false
    if (filter === 'problems' && !row.problem) return false
    if (words.length === 0) return true
    const text = `${row.label} ${row.key} ${row.scenario}`.toLowerCase()
    return words.every((w) => text.includes(w))
  })
}

export interface BehaviourGroup {
  theme: string
  label: string
  rows: BehaviourRow[]
  differences: number
  problems: number
}

// groupBehaviourRows splits rows by theme, in the theme order.
export function groupBehaviourRows(rows: BehaviourRow[]): BehaviourGroup[] {
  const groups = new Map<string, BehaviourGroup>()
  for (const row of rows) {
    let g = groups.get(row.theme)
    if (!g) {
      const theme = BEHAVIOUR_THEMES.find((t) => t.key === row.theme) ?? OTHER_THEME
      g = { theme: theme.key, label: theme.label, rows: [], differences: 0, problems: 0 }
      groups.set(row.theme, g)
    }
    g.rows.push(row)
    if (row.differs) g.differences++
    if (row.problem) g.problems++
  }
  return [...groups.values()].sort((a, b) => themeRank(a.theme) - themeRank(b.theme))
}

// shortValue shortens an observation value for a narrow cell. It keeps the
// text before the first ":" or "(" of an error text when that is long
// enough to read, then cuts at max characters.
export function shortValue(value: string, max = 40): string {
  const v = value.trim()
  if (v.length <= max) return v
  const cut = v.search(/[:(]/)
  const head = cut >= 12 && cut <= max ? v.slice(0, cut).trim() : v
  return head.length <= max ? head : `${head.slice(0, max - 1).trimEnd()}…`
}
