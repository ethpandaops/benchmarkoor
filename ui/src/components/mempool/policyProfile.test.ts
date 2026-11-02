import { describe, it, expect } from 'vitest'
import type { MempoolRunResult } from '@/api/types'
import v2Fixture from './__fixtures__/mempool-result.v2.json'
import { buildPolicyProfile, comparePolicies, limitModel, PROFILE_SOURCES } from './policyProfile'

const base = v2Fixture as unknown as MempoolRunResult

function values(r: MempoolRunResult): Record<string, string | undefined> {
  return Object.fromEntries(buildPolicyProfile(r).map((f) => [f.key, f.value]))
}

function setCheck(r: MempoolRunResult, name: string, passed: boolean) {
  for (const s of r.scenarios) for (const a of s.assertions) if (a.name === name) a.passed = passed
}

function setObservation(r: MempoolRunResult, scenario: string, key: string, value: string) {
  r.scenarios.find((s) => s.name === scenario)!.observations.find((o) => o.key === key)!.value = value
}

describe('buildPolicyProfile', () => {
  it('derives every field from a geth run', () => {
    expect(values(base)).toEqual({
      replacement_bump: '≥ 10%',
      per_sender: '256 of 256',
      queue_per_account: '64 of 80',
      blob_per_sender: '16',
      delegated_inflight: '1',
      far_nonce: 'queued',
      zero_tip: 'no',
      drops_unpayable: 'yes',
      blob_reinject: 'yes',
      cumulative_value: 'yes',
      blob_v0: 'converts',
    })
  })

  it('reads the other outcomes', () => {
    const r = structuredClone(base)
    setCheck(r, PROFILE_SOURCES.replacementAtBump, false)
    setCheck(r, PROFILE_SOURCES.dropsUnpayable, false)
    setObservation(r, 'nonce-gap-promotion', 'far_nonce', 'nonce too high')
    setObservation(r, 'base-fee-movement', 'zero_tip.included', 'true')
    setObservation(r, 'admission-rejections', 'reject.blob_v0_sidecar', 'invalid sidecar version')
    const v = values(r)
    expect(v.replacement_bump).toBe('> 10%')
    expect(v.drops_unpayable).toBe('no')
    expect(v.far_nonce).toBe('rejected')
    expect(v.zero_tip).toBe('yes')
    expect(v.blob_v0).toBe('rejects')
  })

  it('leaves fields empty when the data is missing', () => {
    const r = structuredClone(base)
    r.scenarios = []
    const fields = buildPolicyProfile(r)
    expect(fields).toHaveLength(11)
    expect(fields.every((f) => f.value === undefined)).toBe(true)
    expect(fields.every((f) => f.hint.length > 0)).toBe(true)
  })
})

describe('limitModel', () => {
  it('knows the five clients', () => {
    for (const c of ['geth', 'besu', 'nethermind', 'reth', 'erigon']) expect(limitModel(c)).toBeTruthy()
    expect(limitModel('Geth')).toBe(limitModel('geth'))
    expect(limitModel('unknown')).toBeUndefined()
  })
})

describe('comparePolicies', () => {
  it('marks only the fields where known values differ', () => {
    const besu = structuredClone(base)
    setCheck(besu, PROFILE_SOURCES.replacementAtBump, false)
    const rows = comparePolicies([
      { client: 'geth', result: base },
      { client: 'besu', result: besu },
    ])
    const byKey = Object.fromEntries(rows.map((r) => [r.key, r]))
    expect(rows[0].key).toBe('limit_model')
    expect(rows[0].differs).toBe(false)
    expect(byKey.replacement_bump.values).toEqual({ geth: '≥ 10%', besu: '> 10%' })
    expect(byKey.replacement_bump.differs).toBe(true)
    expect(rows.filter((r) => r.differs).map((r) => r.key)).toEqual(['replacement_bump'])
  })

  it('does not count a missing value as a difference', () => {
    const old = structuredClone(base)
    old.scenarios = old.scenarios.filter((s) => s.name !== 'blob-pool')
    const rows = comparePolicies([
      { client: 'geth', result: base },
      { client: 'reth', result: old },
    ])
    const blob = rows.find((r) => r.key === 'blob_per_sender')!
    expect(blob.values.reth).toBeUndefined()
    expect(blob.differs).toBe(false)
  })
})
