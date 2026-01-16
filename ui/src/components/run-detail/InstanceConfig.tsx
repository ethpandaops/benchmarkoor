import { useState } from 'react'
import clsx from 'clsx'
import type { InstanceConfig as InstanceConfigType } from '@/api/types'
import { ClientBadge } from '@/components/shared/ClientBadge'

interface InstanceConfigProps {
  instance: InstanceConfigType
}

export function InstanceConfig({ instance }: InstanceConfigProps) {
  const [expanded, setExpanded] = useState(false)

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 text-left hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-700/50"
      >
        <h3 className="shrink-0 text-sm/6 font-medium text-gray-900 dark:text-gray-100">Instance Configuration</h3>
        <div className="flex min-w-0 items-center gap-3">
          <span className="truncate font-mono text-xs/5 text-gray-500 dark:text-gray-400">{instance.image}</span>
          <svg
            className={clsx('size-5 shrink-0 text-gray-500 transition-transform', expanded && 'rotate-180')}
            fill="none"
            viewBox="0 0 24 24"
            stroke="currentColor"
          >
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
          </svg>
        </div>
      </button>
      {expanded && (
        <div className="p-4">
          <div className="flex flex-col gap-4">
            <div className="flex items-center gap-4">
              <div>
                <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Client</dt>
                <dd className="mt-1">
                  <ClientBadge client={instance.client} />
                </dd>
              </div>
              <div>
                <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Instance ID</dt>
                <dd className="mt-1 text-sm/6 text-gray-900 dark:text-gray-100">{instance.id}</dd>
              </div>
            </div>

            <div>
              <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Image</dt>
              <dd className="mt-1 font-mono text-sm/6 text-gray-900 dark:text-gray-100">{instance.image}</dd>
            </div>

            {instance.genesis && (
              <div>
                <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Genesis</dt>
                <dd className="mt-1 break-all font-mono text-sm/6 text-gray-900 dark:text-gray-100">{instance.genesis}</dd>
              </div>
            )}

            {instance.command && instance.command.length > 0 && (
              <div>
                <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Command</dt>
                <dd className="mt-1 overflow-x-auto rounded-sm bg-gray-100 p-2 font-mono text-xs/5 text-gray-900 dark:bg-gray-900 dark:text-gray-100">
                  {instance.command.join(' ')}
                </dd>
              </div>
            )}

            {instance.extra_args && instance.extra_args.length > 0 && (
              <div>
                <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Extra Arguments</dt>
                <dd className="mt-1 overflow-x-auto rounded-sm bg-gray-100 p-2 font-mono text-xs/5 text-gray-900 dark:bg-gray-900 dark:text-gray-100">
                  {instance.extra_args.join(' ')}
                </dd>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
