import { Link } from '@tanstack/react-router'
import clsx from 'clsx'
import type { RunConfig } from '@/api/types'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { StrategyIcon } from '@/components/shared/StrategyIcon'
import { StatusBadge } from '@/components/shared/StatusBadge'
import { formatTimestamp, formatRelativeTime } from '@/utils/date'
import { type CompareRun, RUN_SLOTS } from './constants'

interface CompareHeaderProps {
  runs: CompareRun[]
}

function RunCard({
  config,
  runId,
  label,
  accentClass,
}: {
  config: RunConfig
  runId: string
  label: string
  accentClass: string
}) {
  return (
    <div className={clsx('flex-1 rounded-sm border-t-3 bg-white p-4 shadow-xs dark:bg-gray-800', accentClass)}>
      <div className="mb-2 flex items-center gap-2">
        <span className="text-xs/5 font-semibold uppercase tracking-wider text-gray-500 dark:text-gray-400">
          {label}
        </span>
        <StatusBadge status={config.status} />
      </div>
      <div className="flex items-center gap-3">
        <ClientBadge client={config.instance.client} />
        <StrategyIcon strategy={config.instance.rollback_strategy} />
      </div>
      <div className="mt-3 flex flex-col gap-1">
        <div className="flex min-w-0 items-center gap-2">
          <span className="shrink-0 text-xs/5 text-gray-500 dark:text-gray-400">Run:</span>
          <Link
            to="/runs/$runId"
            params={{ runId }}
            className="truncate font-mono text-sm/6 text-blue-600 hover:text-blue-800 hover:underline dark:text-blue-400 dark:hover:text-blue-300"
            title={runId}
          >
            {runId}
          </Link>
        </div>
        {config.instance.client_version && (
          <div className="flex min-w-0 items-center gap-2">
            <span className="shrink-0 text-xs/5 text-gray-500 dark:text-gray-400">Version:</span>
            <span className="truncate font-mono text-sm/6 text-gray-900 dark:text-gray-100" title={config.instance.client_version}>
              {config.instance.client_version}
            </span>
          </div>
        )}
        <div className="flex min-w-0 items-center gap-2">
          <span className="shrink-0 text-xs/5 text-gray-500 dark:text-gray-400">Image:</span>
          <span className="truncate font-mono text-sm/6 text-gray-900 dark:text-gray-100" title={config.instance.image}>
            {config.instance.image}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">Time:</span>
          <span className="text-sm/6 text-gray-900 dark:text-gray-100" title={formatRelativeTime(config.timestamp)}>
            {formatTimestamp(config.timestamp)}
          </span>
        </div>
      </div>
    </div>
  )
}

const GRID_COLS: Record<number, string> = {
  2: 'grid-cols-1 lg:grid-cols-2',
  3: 'grid-cols-1 lg:grid-cols-3',
  4: 'grid-cols-1 lg:grid-cols-2 xl:grid-cols-4',
  5: 'grid-cols-1 lg:grid-cols-2 xl:grid-cols-5',
}

export function CompareHeader({ runs }: CompareHeaderProps) {
  return (
    <div className={clsx('grid gap-4', GRID_COLS[runs.length] ?? GRID_COLS[2])}>
      {runs.map((run) => {
        const slot = RUN_SLOTS[run.index]
        return (
          <RunCard
            key={run.runId}
            config={run.config}
            runId={run.runId}
            label={`Run ${slot.label}`}
            accentClass={slot.borderClass}
          />
        )
      })}
    </div>
  )
}
