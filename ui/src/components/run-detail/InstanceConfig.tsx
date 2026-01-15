import type { InstanceConfig as InstanceConfigType } from '@/api/types'
import { Card } from '@/components/shared/Card'
import { ClientBadge } from '@/components/shared/ClientBadge'

interface InstanceConfigProps {
  instance: InstanceConfigType
}

export function InstanceConfig({ instance }: InstanceConfigProps) {
  return (
    <Card title="Instance Configuration" collapsible defaultCollapsed>
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
    </Card>
  )
}
