import type { SourceInfo } from '@/api/types'
import { Card } from '@/components/shared/Card'

interface SuiteSourceProps {
  title: string
  source: SourceInfo
}

export function SuiteSource({ title, source }: SuiteSourceProps) {
  if (source.git) {
    return (
      <Card title={title} collapsible>
        <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2">
          <div>
            <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Repository</dt>
            <dd className="mt-1 break-all font-mono text-sm/6 text-gray-900 dark:text-gray-100">
              <a
                href={source.git.repo}
                target="_blank"
                rel="noopener noreferrer"
                className="text-blue-600 hover:text-blue-800 dark:text-blue-400 dark:hover:text-blue-300"
              >
                {source.git.repo}
              </a>
            </dd>
          </div>
          <div>
            <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Version</dt>
            <dd className="mt-1 font-mono text-sm/6 text-gray-900 dark:text-gray-100">{source.git.version}</dd>
          </div>
          {source.git.directory && (
            <div>
              <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Directory</dt>
              <dd className="mt-1 font-mono text-sm/6 text-gray-900 dark:text-gray-100">{source.git.directory}</dd>
            </div>
          )}
          <div>
            <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Commit SHA</dt>
            <dd className="mt-1 font-mono text-sm/6 text-gray-900 dark:text-gray-100">{source.git.sha}</dd>
          </div>
        </dl>
      </Card>
    )
  }

  if (source.local_dir) {
    return (
      <Card title={title} collapsible>
        <div>
          <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Local Directory</dt>
          <dd className="mt-1 font-mono text-sm/6 text-gray-900 dark:text-gray-100">{source.local_dir}</dd>
        </div>
      </Card>
    )
  }

  return null
}
