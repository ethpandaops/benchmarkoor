import { describe, expect, it } from 'vitest'

import type { CPUTopologyEntry } from '@/api/types'

import { cpuLayoutSummary, groupCores, parseCpuList } from './cpuTopology'

// sixCoresSMT is a 6-core host with 2 threads per core, siblings (n, n+6).
function sixCoresSMT(): CPUTopologyEntry[] {
  return Array.from({ length: 12 }, (_, id) => ({ id, core: id % 6, socket: 0, numa: 0 }))
}

const twoSockets: CPUTopologyEntry[] = [
  { id: 0, core: 0, socket: 0, numa: 0 },
  { id: 1, core: 0, socket: 0, numa: 0 },
  { id: 2, core: 1, socket: 0, numa: 0 },
  { id: 3, core: 1, socket: 0, numa: 0 },
  { id: 4, core: 0, socket: 1, numa: 1 },
  { id: 5, core: 0, socket: 1, numa: 1 },
  { id: 6, core: 1, socket: 1, numa: 1 },
  { id: 7, core: 1, socket: 1, numa: 1 },
]

describe('parseCpuList', () => {
  it('parses singles and ranges', () => {
    expect([...parseCpuList('8,0-2,10-11')].sort((a, b) => a - b)).toEqual([0, 1, 2, 8, 10, 11])
  })

  it('returns an empty set for missing or malformed input', () => {
    expect(parseCpuList(undefined).size).toBe(0)
    expect(parseCpuList('')).toEqual(new Set())
    expect(parseCpuList('a-b,3')).toEqual(new Set([3]))
  })
})

describe('groupCores', () => {
  it('groups threads by socket, numa and core in id order', () => {
    const groups = groupCores([...twoSockets].reverse())
    expect(groups.map((g) => [g.socket, g.numa])).toEqual([
      [0, 0],
      [1, 1],
    ])
    expect(groups[0].cores.map((c) => c.threads.map((t) => t.id))).toEqual([
      [0, 1],
      [2, 3],
    ])
    expect(groups[1].cores.map((c) => c.threads.map((t) => t.id))).toEqual([
      [4, 5],
      [6, 7],
    ])
  })
})

describe('cpuLayoutSummary', () => {
  it('returns an empty string without topology', () => {
    expect(cpuLayoutSummary(undefined, '0-3')).toBe('')
    expect(cpuLayoutSummary([], '0-3')).toBe('')
  })

  it('describes the host without a cpuset', () => {
    expect(cpuLayoutSummary(sixCoresSMT())).toBe('12 threads on 6 physical cores (2 threads per core)')
  })

  it('shows one thread per core spread across every core', () => {
    expect(cpuLayoutSummary(sixCoresSMT(), '0-5')).toBe(
      '6 threads on 6 of 6 physical cores (1 of 2 threads per core)',
    )
  })

  it('shows full cores when siblings are pinned together', () => {
    expect(cpuLayoutSummary(sixCoresSMT(), '0,6,1,7,2,8')).toBe(
      '6 threads on 3 of 6 physical cores (2 of 2 threads per core)',
    )
  })

  it('shows a range for uneven usage', () => {
    expect(cpuLayoutSummary(sixCoresSMT(), '0,6,1')).toBe(
      '3 threads on 2 of 6 physical cores (1-2 of 2 threads per core)',
    )
  })

  it('uses singular for one thread', () => {
    expect(cpuLayoutSummary(sixCoresSMT(), '4')).toBe(
      '1 thread on 1 of 6 physical cores (1 of 2 threads per core)',
    )
  })

  it('reports a cpuset outside the host', () => {
    expect(cpuLayoutSummary(sixCoresSMT(), '99')).toBe('cpuset matches none of the 12 host threads')
  })

  it('appends socket and NUMA counts on multi-socket hosts', () => {
    expect(cpuLayoutSummary(twoSockets)).toBe(
      '8 threads on 4 physical cores (2 threads per core), 2 sockets, 2 NUMA nodes',
    )
    expect(cpuLayoutSummary(twoSockets, '4-7')).toBe(
      '4 threads on 2 of 4 physical cores (2 of 2 threads per core), 1 of 2 sockets, 1 of 2 NUMA nodes',
    )
  })
})
