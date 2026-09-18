import type { CPUTopologyEntry } from '@/api/types'

/** A physical core and the logical CPUs (threads) it hosts. */
export interface PhysicalCore {
  socket: number
  numa: number
  core: number
  threads: CPUTopologyEntry[]
}

/** A group of physical cores that share a socket and NUMA node. */
export interface CoreGroup {
  socket: number
  numa: number
  cores: PhysicalCore[]
}

// parseCpuList parses a kernel/docker cpuset such as "0-3,8,10-11" into a
// sorted set of CPU IDs. Malformed entries are skipped.
export function parseCpuList(list: string | undefined): Set<number> {
  const ids = new Set<number>()
  if (!list) return ids
  for (const part of list.split(',')) {
    const item = part.trim()
    if (!item) continue
    const [start, end] = item.split('-')
    const from = Number(start)
    const to = end === undefined ? from : Number(end)
    if (!Number.isInteger(from) || !Number.isInteger(to) || to < from) continue
    for (let id = from; id <= to; id++) ids.add(id)
  }
  return ids
}

// groupCores arranges a flat topology into socket/NUMA groups of physical
// cores, each with its threads in ID order. Groups, cores and threads are
// all sorted so the layout is stable.
export function groupCores(topology: CPUTopologyEntry[]): CoreGroup[] {
  const groups = new Map<string, CoreGroup>()
  const cores = new Map<string, PhysicalCore>()

  for (const cpu of topology) {
    const groupKey = `${cpu.socket}:${cpu.numa}`
    let group = groups.get(groupKey)
    if (!group) {
      group = { socket: cpu.socket, numa: cpu.numa, cores: [] }
      groups.set(groupKey, group)
    }

    const coreKey = `${groupKey}:${cpu.core}`
    let core = cores.get(coreKey)
    if (!core) {
      core = { socket: cpu.socket, numa: cpu.numa, core: cpu.core, threads: [] }
      cores.set(coreKey, core)
      group.cores.push(core)
    }
    core.threads.push(cpu)
  }

  const result = [...groups.values()].sort((a, b) => a.socket - b.socket || a.numa - b.numa)
  for (const group of result) {
    group.cores.sort((a, b) => a.core - b.core)
    for (const core of group.cores) core.threads.sort((a, b) => a.id - b.id)
  }
  return result
}

function plural(count: number, unit: string): string {
  return `${count} ${unit}${count === 1 ? '' : 's'}`
}

// cpuLayoutSummary describes a cpuset in terms of physical cores, for
// example "6 threads on 3 of 6 physical cores (2 of 2 threads per core)".
// With an empty cpuset it describes the host instead. The wording matches
// cputopology.Summary in Go so logs, markdown and UI agree.
export function cpuLayoutSummary(topology: CPUTopologyEntry[] | undefined, cpuset?: string): string {
  if (!topology || topology.length === 0) return ''

  const pinned = parseCpuList(cpuset)
  const groups = groupCores(topology)
  const cores = groups.flatMap((g) => g.cores)
  const sockets = new Set(topology.map((c) => c.socket))
  const nodes = new Set(topology.map((c) => c.numa))
  const hostThreadsPerCore = Math.max(...cores.map((c) => c.threads.length))

  if (pinned.size === 0) {
    let s = `${topology.length} threads on ${cores.length} physical cores (${plural(hostThreadsPerCore, 'thread')} per core)`
    if (sockets.size > 1) s += `, ${sockets.size} sockets`
    if (nodes.size > 1) s += `, ${nodes.size} NUMA nodes`
    return s
  }

  const usedCores = cores
    .map((c) => ({ core: c, used: c.threads.filter((t) => pinned.has(t.id)).length }))
    .filter((c) => c.used > 0)
  const usedThreads = usedCores.reduce((sum, c) => sum + c.used, 0)
  if (usedThreads === 0) return `cpuset matches none of the ${topology.length} host threads`

  const minUsed = Math.min(...usedCores.map((c) => c.used))
  const maxUsed = Math.max(...usedCores.map((c) => c.used))
  const perCore = minUsed === maxUsed ? `${minUsed}` : `${minUsed}-${maxUsed}`
  const usedSockets = new Set(usedCores.map((c) => c.core.socket))
  const usedNodes = new Set(usedCores.map((c) => c.core.numa))

  let s = `${plural(usedThreads, 'thread')} on ${usedCores.length} of ${cores.length} physical cores (${perCore} of ${hostThreadsPerCore} threads per core)`
  if (sockets.size > 1) s += `, ${usedSockets.size} of ${sockets.size} sockets`
  if (nodes.size > 1) s += `, ${usedNodes.size} of ${nodes.size} NUMA nodes`
  return s
}
