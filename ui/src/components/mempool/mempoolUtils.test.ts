import { describe, it, expect } from 'vitest'
import type { IndexEntry, MempoolRunResult } from '@/api/types'
import gethFixture from './__fixtures__/mempool-result.geth.json'
import {
  buildComparisonRows,
  computeDifferences,
  histogramBuckets,
  isMempoolMode,
  rankValues,
  selectMempoolPeerRuns,
  sortScenarios,
  splitEventMarkers,
  summarizeMempoolResult,
  formatMempoolValue,
  type ClientMempoolResult,
} from './mempoolUtils'

const geth = gethFixture as MempoolRunResult

// rethVariant clones the geth fixture and changes a few values so the
// comparison has something to find.
function rethVariant(): MempoolRunResult {
  const r = structuredClone(geth)
  r.client = 'reth'
  r.instance_id = 'reth'
  const admission = r.scenarios.find((s) => s.name === 'admission')!
  admission.metrics.find((m) => m.key === 'admit_rate')!.value = 2500
  admission.latencies.find((l) => l.key === 'send_raw')!.p99 = 3.1
  const rejections = r.scenarios.find((s) => s.name === 'admission-rejections')!
  rejections.observations.find((o) => o.key === 'nonce_too_low')!.value = 'nonce too low'
  rejections.assertions.find((a) => a.name === 'oversized_error_code')!.passed = true
  return r
}

function entry(runId: string, client: string, timestamp: number, extra: Partial<IndexEntry> = {}): IndexEntry {
  return {
    run_id: runId,
    timestamp,
    suite_hash: 'suite',
    instance: { id: client, client, image: client },
    tests: { tests_total: 0, tests_passed: 0, tests_failed: 0, steps: {} },
    mode: 'mempool',
    ...extra,
  }
}

describe('isMempoolMode', () => {
  it('treats an absent mode as replay', () => {
    expect(isMempoolMode({})).toBe(false)
    expect(isMempoolMode(undefined)).toBe(false)
    expect(isMempoolMode({ mode: 'mempool' })).toBe(true)
  })
})

describe('summarizeMempoolResult', () => {
  it('counts scenario states and findings', () => {
    expect(summarizeMempoolResult(geth)).toEqual({
      total: 7,
      passed: 5,
      failed: 1,
      error: 1,
      hardFailures: 1,
      // Only the failed soft assertion; observations are counted apart.
      findings: 1,
      observations: 6,
    })
  })
})

describe('sortScenarios', () => {
  it('puts error and failed scenarios first and keeps the suite order', () => {
    expect(sortScenarios(geth.scenarios).map((s) => s.name)).toEqual([
      'reorg-reinjection',
      'replacement',
      'admission',
      'admission-rejections',
      'nonce-gap-promotion',
      'payload-ordering',
      'import-full-pool',
    ])
  })
})

describe('selectMempoolPeerRuns', () => {
  it('keeps the current run and the latest completed mempool run of other clients', () => {
    const entries = [
      entry('g-old', 'geth', 1),
      entry('g-cur', 'geth', 2),
      entry('g-new', 'geth', 3),
      entry('r-1', 'reth', 1),
      entry('r-2', 'reth', 5),
      entry('r-live', 'reth', 9, { status: 'running' }),
      entry('n-replay', 'nethermind', 9, { mode: undefined }),
      entry('b-other', 'besu', 9, { suite_hash: 'other' }),
      entry('e-del', 'erigon', 9, { deletion_requested_at: 1 }),
    ]
    expect(selectMempoolPeerRuns(entries, 'suite', 'g-cur').map((e) => e.run_id)).toEqual(['g-cur', 'r-2'])
  })

  it('returns nothing without a suite hash', () => {
    expect(selectMempoolPeerRuns([entry('a', 'geth', 1)], undefined, 'a')).toEqual([])
  })
})

describe('rankValues', () => {
  it('marks best and worst by direction', () => {
    expect(rankValues({ a: 1, b: 2, c: 3 }, 'lower')).toEqual({ best: ['a'], worst: ['c'] })
    expect(rankValues({ a: 1, b: 2, c: 3 }, 'higher')).toEqual({ best: ['c'], worst: ['a'] })
  })

  it('marks nothing without direction, spread or a second value', () => {
    expect(rankValues({ a: 1, b: 2 })).toEqual({ best: [], worst: [] })
    expect(rankValues({ a: 2, b: 2 }, 'lower')).toEqual({ best: [], worst: [] })
    expect(rankValues({ a: 2, b: undefined }, 'lower')).toEqual({ best: [], worst: [] })
  })
})

describe('buildComparisonRows', () => {
  const results: ClientMempoolResult[] = [
    { client: 'geth', runId: 'g', result: geth },
    { client: 'reth', runId: 'r', result: rethVariant() },
  ]
  const rows = buildComparisonRows(results)

  it('adds metric rows and p50/p99 latency rows per scenario', () => {
    const admission = rows.filter((r) => r.scenario === 'admission').map((r) => r.key)
    expect(admission).toEqual([
      'txs_sent',
      'admitted',
      'admit_rate',
      'peak_memory',
      'send_raw.p50',
      'send_raw.p99',
      'visible.p50',
      'visible.p99',
    ])
  })

  it('ranks the clients with the metric direction', () => {
    const rate = rows.find((r) => r.scenario === 'admission' && r.key === 'admit_rate')!
    expect(rate.values).toEqual({ geth: 1904.7, reth: 2500 })
    expect(rate.best).toEqual(['reth'])
    expect(rate.worst).toEqual(['geth'])

    const p99 = rows.find((r) => r.scenario === 'admission' && r.key === 'send_raw.p99')!
    expect(p99.values.geth).toBeCloseTo(7.74)
    expect(p99.best).toEqual(['reth'])
    expect(p99.worst).toEqual(['geth'])
  })

  it('leaves equal values unranked', () => {
    const p50 = rows.find((r) => r.scenario === 'admission' && r.key === 'send_raw.p50')!
    expect(p50.best).toEqual([])
  })
})

describe('computeDifferences', () => {
  it('lists assertions and observations that differ between clients', () => {
    const diffs = computeDifferences([
      { client: 'geth', runId: 'g', result: geth },
      { client: 'reth', runId: 'r', result: rethVariant() },
    ])
    expect(diffs).toHaveLength(1)
    expect(diffs[0].scenario).toBe('admission-rejections')
    expect(diffs[0].assertions.map((a) => [a.name, a.values])).toEqual([
      ['oversized_error_code', { geth: false, reth: true }],
    ])
    expect(diffs[0].observations.map((o) => o.key)).toEqual(['nonce_too_low'])
  })

  it('returns nothing for a single client', () => {
    expect(computeDifferences([{ client: 'geth', runId: 'g', result: geth }])).toEqual([])
  })
})

describe('histogramBuckets', () => {
  it('keeps per-bucket counts as they are', () => {
    const h = [{ le: 1, count: 2 }, { le: 2, count: 5 }, { le: 5, count: 3 }]
    expect(histogramBuckets({ count: 10, histogram: h })).toEqual(h)
  })

  it('converts cumulative counts to per-bucket counts', () => {
    const h = [{ le: 1, count: 2 }, { le: 2, count: 7 }, { le: 5, count: 10 }]
    expect(histogramBuckets({ count: 10, histogram: h }).map((b) => b.count)).toEqual([2, 5, 3])
  })
})

describe('splitEventMarkers', () => {
  it('collapses kinds with too many events', () => {
    const admission = geth.scenarios.find((s) => s.name === 'admission')!
    const { markers, collapsed } = splitEventMarkers(admission.events)
    expect(markers.map((e) => e.kind)).toEqual(['settled'])
    expect(collapsed).toEqual([{ kind: 'send', count: 200 }])
  })
})

describe('formatMempoolValue', () => {
  it('formats by unit', () => {
    expect(formatMempoolValue(undefined, 'ms')).toBe('-')
    expect(formatMempoolValue(1.234, 'ms')).toBe('1.23 ms')
    expect(formatMempoolValue(1500, 'ms')).toBe('1.50 s')
    expect(formatMempoolValue(2048, 'bytes')).toBe('2.0 KB')
  })
})
