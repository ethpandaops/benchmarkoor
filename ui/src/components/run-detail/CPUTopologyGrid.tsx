import clsx from 'clsx'
import type { CPUTopologyEntry } from '@/api/types'
import { cpuLayoutSummary, groupCores, parseCpuList, type PhysicalCore } from '@/utils/cpuTopology'

interface CPUTopologyGridProps {
  topology: CPUTopologyEntry[]
  /** The cpuset the run was pinned to, e.g. "0,6,1,7". Empty shows the host only. */
  cpuset?: string
}

type CoreUsage = 'full' | 'partial' | 'none'

function coreUsage(core: PhysicalCore, pinned: Set<number>): CoreUsage {
  const used = core.threads.filter((t) => pinned.has(t.id)).length
  if (used === 0) return 'none'
  return used === core.threads.length ? 'full' : 'partial'
}

// CoreBox draws one physical core as a bordered box with one cell per
// hardware thread. The border tells whether the run used all, some, or none
// of the core's threads; the cell tells whether that thread was pinned.
function CoreBox({ core, pinned }: { core: PhysicalCore; pinned: Set<number> }) {
  const usage = coreUsage(core, pinned)

  return (
    <div
      className={clsx(
        'flex gap-0.5 rounded-xs border p-0.5',
        usage === 'full' && 'border-blue-500 dark:border-blue-400',
        usage === 'partial' && 'border-dashed border-amber-500 dark:border-amber-400',
        usage === 'none' && 'border-gray-300 dark:border-gray-600',
      )}
      title={`core ${core.core} · socket ${core.socket} · NUMA ${core.numa}`}
    >
      {core.threads.map((thread) => {
        const isPinned = pinned.has(thread.id)
        return (
          <span
            key={thread.id}
            className={clsx(
              'flex h-5 min-w-6 items-center justify-center rounded-xs px-1 font-mono text-xs/5 tabular-nums',
              isPinned
                ? 'bg-blue-600 text-white dark:bg-blue-500'
                : 'bg-gray-100 text-gray-500 dark:bg-gray-700 dark:text-gray-400',
            )}
            title={`CPU ${thread.id} · core ${core.core} · socket ${core.socket} · NUMA ${core.numa}${isPinned ? ' · pinned' : ''}`}
          >
            {thread.id}
          </span>
        )
      })}
    </div>
  )
}

function LegendItem({ swatch, label }: { swatch: string; label: string }) {
  return (
    <span className="flex items-center gap-1.5">
      <span className={clsx('size-3 rounded-xs', swatch)} />
      {label}
    </span>
  )
}

export function CPUTopologyGrid({ topology, cpuset }: CPUTopologyGridProps) {
  const pinned = parseCpuList(cpuset)
  const groups = groupCores(topology)
  const summary = cpuLayoutSummary(topology, cpuset)
  const showGroupLabels = groups.length > 1

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <span className="text-sm/6 text-gray-900 dark:text-gray-100">{summary}</span>
        {pinned.size > 0 && (
          <div className="flex flex-wrap gap-x-4 gap-y-1 text-xs/5 text-gray-500 dark:text-gray-400">
            <LegendItem swatch="bg-blue-600 dark:bg-blue-500" label="pinned thread" />
            <LegendItem swatch="border border-blue-500 dark:border-blue-400" label="full core" />
            <LegendItem swatch="border border-dashed border-amber-500 dark:border-amber-400" label="partial core" />
          </div>
        )}
      </div>
      <div className="flex flex-col gap-2">
        {groups.map((group) => (
          <div key={`${group.socket}:${group.numa}`}>
            {showGroupLabels && (
              <div className="mb-1 text-xs/5 text-gray-500 dark:text-gray-400">
                socket {group.socket} · NUMA {group.numa}
              </div>
            )}
            <div className="flex flex-wrap gap-1.5">
              {group.cores.map((core) => (
                <CoreBox key={core.core} core={core} pinned={pinned} />
              ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
