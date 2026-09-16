import { describe, it, expect } from 'vitest'
import type { IndexEntry } from '@/api/types'
import { selectClientPeerRuns } from './clientPeerRuns'

function entry(runId: string, client: string, metadata?: Record<string, string>, instanceId = client): IndexEntry {
  return {
    run_id: runId,
    timestamp: 0,
    suite_hash: 'suite',
    instance: { id: instanceId, client, image: client },
    tests: { tests_total: 1, tests_passed: 1, tests_failed: 0, steps: {} },
    metadata,
  }
}

const ids = (runs: IndexEntry[]) => runs.map((r) => r.run_id)

describe('selectClientPeerRuns', () => {
  const entries = [
    entry('a1', 'geth', { env: 'a' }),
    entry('a2', 'geth', { env: 'a' }),
    entry('b1', 'geth', { env: 'b' }),
    entry('n1', 'geth'),
    entry('r1', 'reth', { env: 'a' }),
    { ...entry('o1', 'geth', { env: 'a' }), suite_hash: 'other' },
  ]

  it('keeps to the runs with the same labels when there are any', () => {
    expect(ids(selectClientPeerRuns(entries, 'suite', 'geth', 'geth', { env: 'a' }))).toEqual(['a1', 'a2'])
  })

  it('widens to every run of the client when no other run shares the labels', () => {
    // The live view never scoped by labels, so a run whose labels are
    // unique must still get a strip on the detail page.
    expect(ids(selectClientPeerRuns(entries, 'suite', 'geth', 'geth', { env: 'b' }))).toEqual(['a1', 'a2', 'b1', 'n1'])
  })

  it('falls back to the instance id before the whole client', () => {
    // CI labels carry a job id, so no two runs share labels. The
    // instance id still tells a bal-full run from a bal-full-aot run.
    const ci = [
      entry('f1', 'besu', { job: '1', aot: 'false' }, 'besu-bal-full'),
      entry('f2', 'besu', { job: '2', aot: 'false' }, 'besu-bal-full'),
      entry('t1', 'besu', { job: '3', aot: 'true' }, 'besu-bal-full-aot'),
      entry('t2', 'besu', { job: '4', aot: 'true' }, 'besu-bal-full-aot'),
      entry('x1', 'besu', { job: '5' }, 'besu-other'),
    ]
    expect(ids(selectClientPeerRuns(ci, 'suite', 'besu', 'besu-bal-full-aot', { job: '3', aot: 'true' }))).toEqual(['t1', 't2'])
    // A lone instance id still gets the whole client.
    expect(ids(selectClientPeerRuns(ci, 'suite', 'besu', 'besu-other', { job: '5' }))).toEqual(['f1', 'f2', 't1', 't2', 'x1'])
  })

  it('treats no labels as its own label set', () => {
    expect(ids(selectClientPeerRuns(entries, 'suite', 'geth', 'geth', undefined))).toEqual(['a1', 'a2', 'b1', 'n1'])
    expect(ids(selectClientPeerRuns([...entries, entry('n2', 'geth')], 'suite', 'geth', 'geth', {}))).toEqual(['n1', 'n2'])
  })

  it('never crosses suites or clients', () => {
    expect(ids(selectClientPeerRuns(entries, 'suite', 'reth', 'reth', { env: 'a' }))).toEqual(['r1'])
    expect(selectClientPeerRuns(entries, undefined, 'geth', 'geth', undefined)).toEqual([])
    expect(selectClientPeerRuns(entries, 'suite', undefined, undefined, undefined)).toEqual([])
  })
})
