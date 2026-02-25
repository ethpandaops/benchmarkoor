import { Link } from '@tanstack/react-router'
import clsx from 'clsx'
import type { RunConfig } from '@/api/types'
import { ClientBadge } from '@/components/shared/ClientBadge'
import { StrategyIcon } from '@/components/shared/StrategyIcon'
import { StatusBadge } from '@/components/shared/StatusBadge'
import { formatTimestamp, formatRelativeTime } from '@/utils/date'

interface CompareHeaderProps {
  configA: RunConfig
  configB: RunConfig
  runIdA: string
  runIdB: string
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
        <div className="flex items-center gap-2">
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">Run:</span>
          <Link
            to="/runs/$runId"
            params={{ runId }}
            className="font-mono text-sm/6 text-blue-600 hover:text-blue-800 hover:underline dark:text-blue-400 dark:hover:text-blue-300"
          >
            {runId}
          </Link>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs/5 text-gray-500 dark:text-gray-400">Image:</span>
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

export function CompareHeader({ configA, configB, runIdA, runIdB }: CompareHeaderProps) {
  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
      <RunCard config={configA} runId={runIdA} label="Run A" accentClass="border-blue-500" />
      <RunCard config={configB} runId={runIdB} label="Run B" accentClass="border-amber-500" />
    </div>
  )
}
