import { useState } from 'react'
import clsx from 'clsx'
import { Settings, Check, Copy, ChevronDown } from 'lucide-react'
import type { InstanceConfig, SystemInfo } from '@/api/types'
import { formatBytes, formatFrequency } from '@/utils/format'

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
      {copied ? <Check className="size-4" /> : <Copy className="size-4" />}
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
        className="flex w-full cursor-pointer items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 text-left hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-700/50"
      >
        <h3 className="flex shrink-0 items-center gap-2 text-sm/6 font-medium text-gray-900 dark:text-gray-100">
          <Settings className="size-4 text-gray-400 dark:text-gray-500" />
          Configuration
        </h3>
        <div className="flex min-w-0 items-center gap-3">
          <span className="truncate text-xs/5 text-gray-500 dark:text-gray-400">{summary}</span>
          <ChevronDown className={clsx('size-5 shrink-0 text-gray-500 transition-transform', expanded && 'rotate-180')} />
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

              {instance.genesis_groups && Object.keys(instance.genesis_groups).length > 0 && (
                <div>
                  <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">
                    Genesis Groups
                  </dt>
                  <dd className="mt-1 overflow-x-auto rounded-xs bg-gray-100 p-2 dark:bg-gray-900">
                    <div className="flex flex-col gap-1 font-mono text-xs/5 text-gray-900 dark:text-gray-100">
                      {Object.entries(instance.genesis_groups).map(([hash, path]) => (
                        <div key={hash} className="flex items-start gap-2">
                          <span className="break-all">
                            <span className="text-gray-500 dark:text-gray-400">{hash}: </span>
                            {path}
                          </span>
                          <CopyButton text={path} />
                        </div>
                      ))}
                    </div>
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

              {instance.rollback_strategy && instance.rollback_strategy !== 'none' && (
                <div>
                  <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">
                    Rollback Strategy
                  </dt>
                  <dd className="mt-1">
                    <div className="group relative inline-flex items-center gap-2">
                      <span className="font-mono text-sm/6 text-gray-900 dark:text-gray-100">
                        {instance.rollback_strategy}
                      </span>
                      <CopyButton text={instance.rollback_strategy} />
                      <div className="pointer-events-none absolute bottom-full left-0 z-10 mb-2 hidden w-64 rounded-sm bg-gray-900 p-2 text-xs/5 text-gray-100 shadow-sm group-hover:block dark:bg-gray-700">
                        {instance.rollback_strategy === 'rpc-debug-setHead'
                          ? 'Rolls back the client to a previous block via a debug RPC call after each test.'
                          : instance.rollback_strategy === 'container-recreate'
                            ? 'Stops and removes the container after each test, then creates a fresh one with the same configuration.'
                            : `Strategy: ${instance.rollback_strategy}`}
                      </div>
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

              {instance.wait_after_rpc_ready && (
                <div>
                  <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Wait After RPC Ready</dt>
                  <dd className="mt-1">
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-sm/6 text-gray-900 dark:text-gray-100">
                        {instance.wait_after_rpc_ready}
                      </span>
                      <CopyButton text={instance.wait_after_rpc_ready} />
                    </div>
                    <p className="mt-1 text-xs text-gray-500 dark:text-gray-400">
                      Waits after RPC becomes ready before running tests, allowing clients to complete internal sync.
                    </p>
                  </dd>
                </div>
              )}

              {instance.retry_new_payloads_syncing_state?.enabled && (
                <div>
                  <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">
                    Retry on SYNCING Response
                  </dt>
                  <dd className="mt-1 overflow-x-auto rounded-sm bg-gray-100 p-2 dark:bg-gray-900">
                    <div className="flex flex-col gap-1 font-mono text-xs/5 text-gray-900 dark:text-gray-100">
                      <div>
                        <span className="text-gray-500 dark:text-gray-400">max_retries: </span>
                        {instance.retry_new_payloads_syncing_state.max_retries}
                      </div>
                      <div>
                        <span className="text-gray-500 dark:text-gray-400">backoff: </span>
                        {instance.retry_new_payloads_syncing_state.backoff}
                      </div>
                    </div>
                    <p className="mt-2 text-xs text-gray-500 dark:text-gray-400">
                      Retries engine_newPayload calls when client returns SYNCING status.
                    </p>
                  </dd>
                </div>
              )}

              {instance.post_test_rpc_calls && instance.post_test_rpc_calls.length > 0 && (
                <div>
                  <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">
                    Post-Test RPC Calls
                  </dt>
                  <dd className="mt-1 flex flex-col gap-2">
                    {instance.post_test_rpc_calls.map((call, i) => (
                      <div
                        key={i}
                        className="overflow-x-auto rounded-sm bg-gray-100 p-2 dark:bg-gray-900"
                      >
                        <div className="flex flex-col gap-1 font-mono text-xs/5 text-gray-900 dark:text-gray-100">
                          <div>
                            <span className="text-gray-500 dark:text-gray-400">method: </span>
                            {call.method}
                          </div>
                          {call.params && call.params.length > 0 && (
                            <div>
                              <span className="text-gray-500 dark:text-gray-400">params: </span>
                              {JSON.stringify(call.params)}
                            </div>
                          )}
                          {call.dump?.enabled && (
                            <div>
                              <span className="text-gray-500 dark:text-gray-400">dump: </span>
                              {call.dump.filename || 'enabled'}
                            </div>
                          )}
                        </div>
                      </div>
                    ))}
                    <p className="text-xs text-gray-500 dark:text-gray-400">
                      Arbitrary RPC calls executed after each test step. Not timed.
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
              <div className="mt-6 border-t border-gray-200 pt-6 dark:border-gray-700">
                <h4 className="mb-3 text-sm/6 font-medium text-gray-900 dark:text-gray-100">Resource Limits</h4>
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
                  {instance.resource_limits.cpu_freq_khz !== undefined && (
                    <InfoItem
                      label="CPU Frequency"
                      value={formatFrequency(instance.resource_limits.cpu_freq_khz)}
                    />
                  )}
                  {instance.resource_limits.cpu_turboboost !== undefined && (
                    <InfoItem
                      label="Turbo Boost"
                      value={instance.resource_limits.cpu_turboboost ? 'Enabled' : 'Disabled'}
                    />
                  )}
                  {instance.resource_limits.cpu_freq_governor && (
                    <InfoItem label="CPU Governor" value={instance.resource_limits.cpu_freq_governor} />
                  )}
                </dl>

                {/* Block I/O Limits */}
                {instance.resource_limits.blkio_config && (
                  <div className="mt-4">
                    <h6 className="mb-2 text-xs/5 font-medium text-gray-500 dark:text-gray-400">Block I/O Limits</h6>
                    <div className="overflow-x-auto rounded-sm bg-gray-100 p-3 dark:bg-gray-900">
                      <div className="flex flex-col gap-3 font-mono text-xs/5 text-gray-900 dark:text-gray-100">
                        {instance.resource_limits.blkio_config.device_read_bps &&
                          instance.resource_limits.blkio_config.device_read_bps.length > 0 && (
                            <div>
                              <span className="text-gray-500 dark:text-gray-400">Read BPS: </span>
                              {instance.resource_limits.blkio_config.device_read_bps.map((dev, i) => (
                                <span key={i}>
                                  {i > 0 && ', '}
                                  {dev.path} @ {formatBytes(dev.rate)}/s
                                </span>
                              ))}
                            </div>
                          )}
                        {instance.resource_limits.blkio_config.device_write_bps &&
                          instance.resource_limits.blkio_config.device_write_bps.length > 0 && (
                            <div>
                              <span className="text-gray-500 dark:text-gray-400">Write BPS: </span>
                              {instance.resource_limits.blkio_config.device_write_bps.map((dev, i) => (
                                <span key={i}>
                                  {i > 0 && ', '}
                                  {dev.path} @ {formatBytes(dev.rate)}/s
                                </span>
                              ))}
                            </div>
                          )}
                        {instance.resource_limits.blkio_config.device_read_iops &&
                          instance.resource_limits.blkio_config.device_read_iops.length > 0 && (
                            <div>
                              <span className="text-gray-500 dark:text-gray-400">Read IOPS: </span>
                              {instance.resource_limits.blkio_config.device_read_iops.map((dev, i) => (
                                <span key={i}>
                                  {i > 0 && ', '}
                                  {dev.path} @ {dev.rate.toLocaleString()}
                                </span>
                              ))}
                            </div>
                          )}
                        {instance.resource_limits.blkio_config.device_write_iops &&
                          instance.resource_limits.blkio_config.device_write_iops.length > 0 && (
                            <div>
                              <span className="text-gray-500 dark:text-gray-400">Write IOPS: </span>
                              {instance.resource_limits.blkio_config.device_write_iops.map((dev, i) => (
                                <span key={i}>
                                  {i > 0 && ', '}
                                  {dev.path} @ {dev.rate.toLocaleString()}
                                </span>
                              ))}
                            </div>
                          )}
                      </div>
                    </div>
                  </div>
                )}
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
