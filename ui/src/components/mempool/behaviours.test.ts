import { describe, it, expect } from 'vitest'
import type { MempoolRunResult } from '@/api/types'
import v2Fixture from './__fixtures__/mempool-result.v2.json'
import {
  buildBehaviourRows,
  cellsDiffer,
  filterBehaviourRows,
  groupBehaviourRows,
  isInvariant,
  shortValue,
  themeOf,
} from './behaviours'
import type { ClientMempoolResult } from './mempoolUtils'

const base = v2Fixture as unknown as MempoolRunResult

function variant(client: string, edit: (r: MempoolRunResult) => void = () => {}): ClientMempoolResult {
  const r = structuredClone(base)
  r.client = client
  edit(r)
  return { client, runId: `run-${client}`, result: r }
}

function scenario(r: MempoolRunResult, name: string) {
  return r.scenarios.find((s) => s.name === name)!
}

describe('isInvariant / themeOf', () => {
  it('detects the invariant prefix', () => {
    expect(isInvariant({ name: 'Invariant: Included txs leave the pool' })).toBe(true)
    expect(isInvariant({ name: 'Included txs leave the pool' })).toBe(false)
  })
  it('maps scenarios to themes', () => {
    expect(themeOf('admission-rejections').label).toBe('Admission')
    expect(themeOf('payload-latency').label).toBe('Ordering & building')
    expect(themeOf('setcode-delegation').label).toBe('EIP-7702')
    expect(themeOf('new-thing').label).toBe('Other')
  })
})

describe('buildBehaviourRows', () => {
  const geth = variant('geth')
  const reth = variant('reth', (r) => {
    const rep = scenario(r, 'replacement')
    rep.assertions.find((a) => a.name === 'Replacement: Fee cap and tip +10%')!.passed = false
    rep.observations.find((o) => o.key === 'replace.at_bump')!.value = 'replacement transaction underpriced'
    // Same outcome, other wording: not a difference.
    rep.observations.find((o) => o.key === 'replace.same_fees')!.value = 'underpriced replacement'
    // Only reth has this observation.
    rep.observations.push({ key: 'replace.extra', label: 'Extra', value: 'accepted' })
  })
  const rows = buildBehaviourRows([geth, reth])

  it('leaves out invariants', () => {
    expect(rows.some((r) => r.label.startsWith('Invariant: '))).toBe(false)
  })

  it('makes one row per check and observation over the union', () => {
    const rep = scenario(base, 'replacement')
    const nonInv = rep.assertions.filter((a) => !isInvariant(a)).length
    const repRows = rows.filter((r) => r.scenario === 'replacement')
    expect(repRows.filter((r) => r.kind === 'check')).toHaveLength(nonInv)
    expect(repRows.filter((r) => r.kind === 'observation')).toHaveLength(rep.observations.length + 1)
    const extra = repRows.find((r) => r.key === 'replace.extra')!
    expect(extra.cells.geth).toBeUndefined()
    expect(extra.differs).toBe(false)
  })

  it('orders rows by theme', () => {
    const themes = [...new Set(rows.map((r) => r.theme))]
    expect(themes).toEqual(['admission', 'nonces', 'replacement', 'limits', 'fees', 'setcode', 'blobs', 'balance'])
  })

  it('marks differences and problems', () => {
    const bump = rows.find((r) => r.label === 'Replacement: Fee cap and tip +10%')!
    expect(bump.differs).toBe(true)
    expect(bump.problem).toBe(true)
    expect(bump.cells.reth).toMatchObject({ kind: 'check', state: 'soft-fail' })
    const atBump = rows.find((r) => r.key === 'replace.at_bump')!
    expect(atBump.differs).toBe(true)
    const sameFees = rows.find((r) => r.key === 'replace.same_fees')!
    expect(sameFees.differs).toBe(false)
    // The fixture has a failed hard check in both clients: a problem, not a difference.
    const hard = rows.find((r) => r.label === 'Original stays after a rejected replacement: Same fees, different content')!
    expect(hard.problem).toBe(true)
    expect(hard.differs).toBe(false)
  })

  it('compares numeric observation values as text', () => {
    expect(cellsDiffer([{ kind: 'observation', value: '3', outcome: 'value' }, { kind: 'observation', value: '4', outcome: 'value' }])).toBe(true)
    expect(cellsDiffer([{ kind: 'observation', value: '3', outcome: 'value' }, undefined])).toBe(false)
  })
})

describe('filterBehaviourRows / groupBehaviourRows', () => {
  const rows = buildBehaviourRows([
    variant('geth'),
    variant('besu', (r) => {
      scenario(r, 'blob-pool').observations.find((o) => o.key === 'per_sender')!.value = '24 / 24 (accepted)'
    }),
  ])

  it('filters by differences, problems and search words', () => {
    const diffs = filterBehaviourRows(rows, 'differences', '')
    expect(diffs.map((r) => r.key)).toEqual(['per_sender'])
    expect(filterBehaviourRows(rows, 'problems', '').every((r) => r.problem)).toBe(true)
    expect(filterBehaviourRows(rows, 'all', 'blob per_sender').map((r) => r.key)).toEqual(['per_sender'])
    expect(filterBehaviourRows(rows, 'all', '').length).toBe(rows.length)
  })

  it('groups by theme with counts', () => {
    const groups = groupBehaviourRows(rows)
    expect(groups[0].label).toBe('Admission')
    const blobs = groups.find((g) => g.theme === 'blobs')!
    expect(blobs.differences).toBe(1)
    expect(groups.reduce((n, g) => n + g.rows.length, 0)).toBe(rows.length)
  })
})

describe('shortValue', () => {
  it('keeps short values', () => {
    expect(shortValue('accepted')).toBe('accepted')
  })
  it('cuts an error text at the first colon', () => {
    expect(shortValue('insufficient funds for gas * price + value: balance 0, tx cost 1', 48)).toBe('insufficient funds for gas * price + value')
  })
  it('truncates long text without a usable break', () => {
    const s = shortValue('a'.repeat(80), 20)
    expect(s).toHaveLength(20)
    expect(s.endsWith('…')).toBe(true)
  })
})
