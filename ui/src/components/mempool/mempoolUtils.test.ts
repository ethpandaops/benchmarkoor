import { describe, it, expect } from 'vitest'
import type { IndexEntry, MempoolRunResult } from '@/api/types'
import gethFixture from './__fixtures__/mempool-result.geth.json'
import {
  barFraction,
  buildComparisonRows,
  classifyObservation,
  computeDifferences,
  countChecks,
  currentFirst,
  durationUnitFor,
  formatDuration,
  formatSig,
  groupEventsByKind,
  groupMetrics,
  histogramBuckets,
  humanizeGroup,
  isMempoolMode,
  orderChecks,
  rankValues,
  relativeToBest,
  selectMempoolPeerRuns,
  sortScenarios,
  splitValueUnit,
  summarizeMempoolResult,
  formatMempoolValue,
  valueFormatter,
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
    // Both texts are rejections: only the wording differs.
    expect(diffs[0].observations).toEqual([])
    expect(diffs[0].wordingOnly.map((o) => o.key)).toEqual(['nonce_too_low'])
  })

  it('splits outcome differences from wording differences', () => {
    const a = structuredClone(geth)
    const b = structuredClone(geth)
    b.client = 'besu'
    const obsA = a.scenarios.find((s) => s.name === 'admission-rejections')!.observations
    const obsB = b.scenarios.find((s) => s.name === 'admission-rejections')!.observations
    obsA.push({ key: 'x.accept', label: 'accept', value: 'accepted' }, { key: 'x.count', label: 'count', value: '6' })
    obsB.push({ key: 'x.accept', label: 'accept', value: 'INVALID: nope' }, { key: 'x.count', label: 'count', value: '7' })
    obsB[0].value = 'some other error text'
    const [d] = computeDifferences([
      { client: 'geth', runId: 'g', result: a },
      { client: 'besu', runId: 'b', result: b },
    ])
    expect(d.observations.map((o) => o.key)).toEqual(['x.accept', 'x.count'])
    expect(d.observations[0].outcomes).toEqual({ geth: 'accepted', besu: 'rejected' })
    expect(d.wordingOnly.map((o) => o.key)).toEqual([obsA[0].key])
  })

  it('returns nothing for a single client', () => {
    expect(computeDifferences([{ client: 'geth', runId: 'g', result: geth }])).toEqual([])
  })
})

describe('classifyObservation', () => {
  it('classifies accepted, rejected and numeric values', () => {
    expect(classifyObservation('accepted')).toBe('accepted')
    expect(classifyObservation('accepted (late)')).toBe('accepted')
    expect(classifyObservation('INTERNAL_ERROR: could not replace existing tx')).toBe('rejected')
    expect(classifyObservation('txn rlp too big: type=2')).toBe('rejected')
    expect(classifyObservation('6')).toBe('value')
    expect(classifyObservation('256 kept, 0 rejected (accepted)')).toBe('value')
    expect(classifyObservation(undefined)).toBeUndefined()
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

describe('groupEventsByKind', () => {
  it('keeps every event and orders the kinds', () => {
    const admission = geth.scenarios.find((s) => s.name === 'admission')!
    const lanes = groupEventsByKind(admission.events)
    expect(lanes.map((l) => l.kind)).toEqual(['send', 'settled'])
    expect(lanes[0].events).toHaveLength(200)
    expect(groupEventsByKind([{ t_ms: 1, kind: 'zzz' }, { t_ms: 2, kind: 'fcu' }]).map((l) => l.kind)).toEqual(['fcu', 'zzz'])
  })
})

describe('formatSig', () => {
  it('keeps 3 significant digits and the integer part', () => {
    expect(formatSig(0)).toBe('0')
    expect(formatSig(0.16349)).toBe('0.163')
    expect(formatSig(5.685)).toBe('5.69')
    expect(formatSig(50.732)).toBe('50.7')
    expect(formatSig(571.7)).toBe('572')
    expect(formatSig(13268.288)).toBe('13,268')
    expect(formatSig(-6.386)).toBe('-6.39')
    expect(formatSig(2.13, 2)).toBe('2.1')
    expect(formatSig(2, 3, true)).toBe('2.00')
  })
})

describe('durations', () => {
  it('picks µs, ms or s per value', () => {
    expect(durationUnitFor(0.163)).toBe('µs')
    expect(durationUnitFor(0)).toBe('ms')
    expect(durationUnitFor(12)).toBe('ms')
    expect(durationUnitFor(1043)).toBe('s')
    expect(formatDuration(0.163)).toBe('163 µs')
    expect(formatDuration(25.793)).toBe('25.8 ms')
    expect(formatDuration(1099.99)).toBe('1.10 s')
    expect(formatDuration(2)).toBe('2.00 ms')
  })
})

describe('formatMempoolValue', () => {
  it('formats by unit', () => {
    expect(formatMempoolValue(undefined, 'ms')).toBe('-')
    expect(formatMempoolValue(1.234, 'ms')).toBe('1.23 ms')
    expect(formatMempoolValue(1500, 'ms')).toBe('1.50 s')
    expect(formatMempoolValue(2048, 'bytes')).toBe('2 KB')
    expect(formatMempoolValue(99.96, '%')).toBe('100%')
    expect(formatMempoolValue(13268.288, 'tx/s')).toBe('13,268 tx/s')
    expect(formatMempoolValue(5120, 'count')).toBe('5,120')
  })

  it('uses one unit per row', () => {
    const f = valueFormatter('ms', [0.5, 12, 3])
    expect([f(0.5), f(12), f(undefined)]).toEqual(['0.500 ms', '12.0 ms', '-'])
  })

  it('splits value and unit', () => {
    expect(splitValueUnit('13,268 tx/s')).toEqual({ value: '13,268', unit: 'tx/s' })
    expect(splitValueUnit('99.9%')).toEqual({ value: '99.9', unit: '%' })
    expect(splitValueUnit('5,120')).toEqual({ value: '5,120', unit: '' })
  })
})

describe('relativeToBest', () => {
  it('describes the distance to the best value', () => {
    expect(relativeToBest(10, 10, 'lower', 'ms')).toBeUndefined()
    expect(relativeToBest(21, 10, 'lower', 'ms')).toBe('2.1× slower')
    expect(relativeToBest(14.8, 10, 'lower', 'ms')).toBe('48% slower')
    expect(relativeToBest(52, 100, 'higher', 'tx/s')).toBe('48% lower')
    expect(relativeToBest(40, 100, 'higher', 'tx/s')).toBe('2.5× lower')
    expect(relativeToBest(3, 1, 'lower', 'blocks')).toBe('3× higher')
    expect(relativeToBest(25, -6.4, 'lower', '%')).toBe('31.4% higher')
    expect(relativeToBest(5, 1, undefined, 'ms')).toBeUndefined()
  })

  it('sizes bars against the row maximum', () => {
    expect(barFraction(5, [5, 10, undefined])).toBe(0.5)
    expect(barFraction(undefined, [5])).toBe(0)
    expect(barFraction(0, [0, 0])).toBe(0)
  })
})

describe('checks', () => {
  const checks = [
    { name: 'a', severity: 'hard' as const, passed: true },
    { name: 'b', severity: 'soft' as const, passed: false },
    { name: 'c', severity: 'hard' as const, passed: false },
    { name: 'd', severity: 'soft' as const, passed: true },
  ]

  it('orders hard failures, soft failures, then passes', () => {
    expect(orderChecks(checks).map((c) => c.name)).toEqual(['c', 'b', 'a', 'd'])
  })

  it('counts by severity', () => {
    expect(countChecks(checks)).toEqual({ hard: { passed: 1, total: 2 }, soft: { passed: 1, total: 2 } })
  })
})

describe('groups', () => {
  it('humanizes group keys', () => {
    expect(humanizeGroup('engine')).toBe('Engine API')
    expect(humanizeGroup('evict')).toBe('Eviction')
    expect(humanizeGroup('blob6')).toBe('Blob ×6')
    expect(humanizeGroup('new_payload')).toBe('newPayload')
    expect(humanizeGroup('some-thing_else')).toBe('Some thing else')
    expect(humanizeGroup(undefined)).toBe('')
  })

  it('groups metrics and moves a shared label suffix to the group', () => {
    const m = (key: string, group: string, label: string) => ({ key, group, label, unit: 'ms', value: 1 })
    const groups = groupMetrics([
      m('a', 'legacy', 'Serial: Legacy'),
      m('b', 'legacy', 'Concurrent: Legacy'),
      m('c', 'blob6', 'Serial: Blob, 6 blobs'),
    ])
    expect(groups.map((g) => [g.heading, g.detail, g.items.map((i) => i.label)])).toEqual([
      ['Legacy', 'Legacy', ['Serial', 'Concurrent']],
      ['Blob ×6', 'Blob, 6 blobs', ['Serial']],
    ])
    expect(groupMetrics([m('a', 'load', 'Rate: x')]).map((g) => [g.heading, g.items[0].label])).toEqual([['', 'Rate: x']])
  })
})

describe('currentFirst', () => {
  it('puts the current run first', () => {
    const items = [{ runId: 'a' }, { runId: 'b' }, { runId: 'c' }]
    expect(currentFirst(items, 'b').map((i) => i.runId)).toEqual(['b', 'a', 'c'])
  })
})
