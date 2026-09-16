import { describe, it, expect } from 'vitest'
import type { IndexEntry } from '@/api/types'
import { selectClientPeerRuns } from './clientPeerRuns'

function entry(runId: string, client: string, metadata?: Record<string, string>): IndexEntry {
  return {
    run_id: runId,
    timestamp: 0,
    suite_hash: 'suite',
    instance: { id: client, client, image: client },
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
    expect(ids(selectClientPeerRuns(entries, 'suite', 'geth', { env: 'a' }))).toEqual(['a1', 'a2'])
  })

  it('widens to every run of the client when no other run shares the labels', () => {
    // The live view never scoped by labels, so a run whose labels are
    // unique must still get a strip on the detail page.
    expect(ids(selectClientPeerRuns(entries, 'suite', 'geth', { env: 'b' }))).toEqual(['a1', 'a2', 'b1', 'n1'])
  })

  it('treats no labels as its own label set', () => {
    expect(ids(selectClientPeerRuns(entries, 'suite', 'geth', undefined))).toEqual(['a1', 'a2', 'b1', 'n1'])
    expect(ids(selectClientPeerRuns([...entries, entry('n2', 'geth')], 'suite', 'geth', {}))).toEqual(['n1', 'n2'])
  })

  it('never crosses suites or clients', () => {
    expect(ids(selectClientPeerRuns(entries, 'suite', 'reth', { env: 'a' }))).toEqual(['r1'])
    expect(selectClientPeerRuns(entries, undefined, 'geth', undefined)).toEqual([])
    expect(selectClientPeerRuns(entries, 'suite', undefined, undefined)).toEqual([])
  })
})
