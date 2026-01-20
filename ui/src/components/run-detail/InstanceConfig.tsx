import { useState } from 'react'
import clsx from 'clsx'
import type { InstanceConfig as InstanceConfigType } from '@/api/types'

interface InstanceConfigProps {
  instance: InstanceConfigType
}

function CopyButton({ text }: { text: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = async () => {
    await navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  return (
    <button
      onClick={handleCopy}
      className="shrink-0 text-gray-400 hover:text-gray-600 dark:hover:text-gray-200"
      title="Copy to clipboard"
    >
      {copied ? (
        <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M5 13l4 4L19 7" />
        </svg>
      ) : (
        <svg className="size-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            d="M8 16H6a2 2 0 01-2-2V6a2 2 0 012-2h8a2 2 0 012 2v2m-6 12h8a2 2 0 002-2v-8a2 2 0 00-2-2h-8a2 2 0 00-2 2v8a2 2 0 002 2z"
          />
        </svg>
      )}
    </button>
  )
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
            <div>
              <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Image</dt>
              <dd className="mt-1 flex items-center gap-2">
                <span className="font-mono text-sm/6 text-gray-900 dark:text-gray-100">{instance.image}</span>
                <CopyButton text={instance.image} />
              </dd>
            </div>

            {instance.genesis && (
              <div>
                <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Genesis</dt>
                <dd className="mt-1 flex items-start gap-2">
                  <span className="break-all font-mono text-sm/6 text-gray-900 dark:text-gray-100">{instance.genesis}</span>
                  <CopyButton text={instance.genesis} />
                </dd>
              </div>
            )}

            {instance.command && instance.command.length > 0 && (
              <div>
                <dt className="flex items-center gap-2 text-xs/5 font-medium text-gray-500 dark:text-gray-400">
                  Command
                  <CopyButton text={instance.command.join(' ')} />
                </dt>
                <dd className="mt-1 overflow-x-auto rounded-sm bg-gray-100 p-2 font-mono text-xs/5 text-gray-900 dark:bg-gray-900 dark:text-gray-100">
                  {instance.command.join(' ')}
                </dd>
              </div>
            )}

            {instance.extra_args && instance.extra_args.length > 0 && (
              <div>
                <dt className="flex items-center gap-2 text-xs/5 font-medium text-gray-500 dark:text-gray-400">
                  Extra Arguments
                  <CopyButton text={instance.extra_args.join(' ')} />
                </dt>
                <dd className="mt-1 overflow-x-auto rounded-sm bg-gray-100 p-2 font-mono text-xs/5 text-gray-900 dark:bg-gray-900 dark:text-gray-100">
                  {instance.extra_args.join(' ')}
                </dd>
              </div>
            )}

            {instance.datadir && (
              <div>
                <dt className="flex items-center gap-2 text-xs/5 font-medium text-gray-500 dark:text-gray-400">
                  Data Directory
                  <CopyButton text={instance.datadir.source_dir} />
                </dt>
                <dd className="mt-1 overflow-x-auto rounded-sm bg-gray-100 p-2 dark:bg-gray-900">
                  <div className="flex flex-col gap-1 font-mono text-xs/5 text-gray-900 dark:text-gray-100">
                    <div>
                      <span className="text-gray-500 dark:text-gray-400">source: </span>
                      {instance.datadir.source_dir}
                    </div>
                    {instance.datadir.container_dir && (
                      <div>
                        <span className="text-gray-500 dark:text-gray-400">mount: </span>
                        {instance.datadir.container_dir}
                      </div>
                    )}
                    {instance.datadir.method && (
                      <div>
                        <span className="text-gray-500 dark:text-gray-400">method: </span>
                        {instance.datadir.method}
                      </div>
                    )}
                  </div>
                </dd>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
