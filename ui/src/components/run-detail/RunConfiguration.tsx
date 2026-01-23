import { useState } from 'react'
import clsx from 'clsx'
import type { InstanceConfig, SystemInfo } from '@/api/types'

interface RunConfigurationProps {
  instance: InstanceConfig
  system: SystemInfo
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

function InfoItem({ label, value }: { label: string; value: string | number }) {
  return (
    <div>
      <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">{label}</dt>
      <dd className="mt-1 flex items-center gap-2 text-sm/6 text-gray-900 dark:text-gray-100">
        <span>{value}</span>
        <CopyButton text={String(value)} />
      </dd>
    </div>
  )
}

export function RunConfiguration({ instance, system }: RunConfigurationProps) {
  const [expanded, setExpanded] = useState(false)

  const summary = `${instance.image} / ${system.platform} (${system.arch}) / ${system.cpu_cores} Cores`

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 text-left hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-700/50"
      >
        <h3 className="shrink-0 text-sm/6 font-medium text-gray-900 dark:text-gray-100">Configuration</h3>
        <div className="flex min-w-0 items-center gap-3">
          <span className="truncate text-xs/5 text-gray-500 dark:text-gray-400">{summary}</span>
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
        <div className="grid grid-cols-1 gap-6 p-4 lg:grid-cols-2">
          {/* Instance Configuration */}
          <div>
            <h4 className="mb-3 text-sm/6 font-medium text-gray-900 dark:text-gray-100">Instance</h4>
            <div className="flex flex-col gap-4">
              <div>
                <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Image</dt>
                <dd className="mt-1 flex items-center gap-2">
                  <span className="font-mono text-sm/6 text-gray-900 dark:text-gray-100">{instance.image}</span>
                  <CopyButton text={instance.image} />
                </dd>
              </div>

              {instance.image_sha256 && (
                <div>
                  <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Image SHA256</dt>
                  <dd className="mt-1 flex items-center gap-2">
                    <span className="font-mono text-sm/6 text-gray-900 dark:text-gray-100">
                      {instance.image_sha256.length > 20
                        ? `${instance.image_sha256.slice(0, 20)}...`
                        : instance.image_sha256}
                    </span>
                    <CopyButton text={instance.image_sha256} />
                  </dd>
                </div>
              )}

              {instance.client_version && (
                <div>
                  <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Client Version</dt>
                  <dd className="mt-1 flex items-center gap-2">
                    <span className="font-mono text-sm/6 text-gray-900 dark:text-gray-100">
                      {instance.client_version}
                    </span>
                    <CopyButton text={instance.client_version} />
                  </dd>
                </div>
              )}

              {instance.genesis && (
                <div>
                  <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Genesis</dt>
                  <dd className="mt-1 flex items-start gap-2">
                    <span className="break-all font-mono text-sm/6 text-gray-900 dark:text-gray-100">
                      {instance.genesis}
                    </span>
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

              {instance.drop_memory_caches && instance.drop_memory_caches !== 'disabled' && (
                <div>
                  <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Drop Memory Caches</dt>
                  <dd className="mt-1">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-sm/6 text-gray-900 dark:text-gray-100">
                        {instance.drop_memory_caches}
                      </span>
                      <CopyButton text={instance.drop_memory_caches} />
                    </div>
                    <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      {instance.drop_memory_caches === 'tests'
                        ? 'Clears Linux page cache between tests for consistent benchmark results.'
                        : 'Clears Linux page cache between each step (setup → test → cleanup) for consistent benchmark results.'}
                    </p>
                  </dd>
                </div>
              )}
            </div>
          </div>

          {/* System Information */}
          <div>
            <h4 className="mb-3 text-sm/6 font-medium text-gray-900 dark:text-gray-100">System</h4>
            <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
              <InfoItem label="Hostname" value={system.hostname} />
              <InfoItem label="OS" value={`${system.platform} ${system.platform_version}`} />
              <InfoItem label="Kernel" value={system.kernel_version} />
              <InfoItem label="Architecture" value={system.arch} />
              <InfoItem label="CPU" value={system.cpu_model} />
              <InfoItem label="CPU Cores" value={system.cpu_cores} />
              <InfoItem label="CPU MHz" value={system.cpu_mhz.toFixed(0)} />
              <InfoItem label="CPU Cache" value={`${system.cpu_cache_kb} KB`} />
              <InfoItem label="Memory" value={`${system.memory_total_gb.toFixed(1)} GB`} />
              {system.virtualization && (
                <InfoItem label="Virtualization" value={`${system.virtualization} (${system.virtualization_role})`} />
              )}
            </dl>

            {/* Resource Limits */}
            {instance.resource_limits && (
              <div className="mt-6">
                <h5 className="mb-3 text-xs/5 font-medium text-gray-500 dark:text-gray-400">Resource Limits</h5>
                <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
                  {instance.resource_limits.cpuset_cpus && (
                    <>
                      <InfoItem
                        label="CPU Count"
                        value={instance.resource_limits.cpuset_cpus.split(',').length}
                      />
                      <InfoItem label="CPU Pinning" value={instance.resource_limits.cpuset_cpus} />
                    </>
                  )}
                  {instance.resource_limits.memory && (
                    <InfoItem label="Memory Limit" value={instance.resource_limits.memory} />
                  )}
                  {instance.resource_limits.swap_disabled !== undefined && (
                    <InfoItem label="Swap Disabled" value={instance.resource_limits.swap_disabled ? 'Yes' : 'No'} />
                  )}
                </dl>
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
