import { useState } from 'react'
import clsx from 'clsx'
import { Settings, ChevronDown } from 'lucide-react'
import type { RunConfig } from '@/api/types'
import { formatBytes, formatFrequency } from '@/utils/format'

interface ConfigDiffProps {
  configA: RunConfig
  configB: RunConfig
}

function DiffRow({ label, valueA, valueB }: { label: string; valueA: string; valueB: string }) {
  const isDifferent = valueA !== valueB
  return (
    <tr className={clsx(isDifferent && 'bg-yellow-50/50 dark:bg-yellow-900/10')}>
      <td className="px-3 py-1.5 text-xs/5 font-medium text-gray-500 dark:text-gray-400">{label}</td>
      <td className={clsx('px-3 py-1.5 font-mono text-xs/5', isDifferent ? 'font-medium text-blue-700 dark:text-blue-300' : 'text-gray-700 dark:text-gray-300')}>
        {valueA || '-'}
      </td>
      <td className={clsx('px-3 py-1.5 font-mono text-xs/5', isDifferent ? 'font-medium text-amber-700 dark:text-amber-300' : 'text-gray-700 dark:text-gray-300')}>
        {valueB || '-'}
      </td>
    </tr>
  )
}

export function ConfigDiff({ configA, configB }: ConfigDiffProps) {
  const [expanded, setExpanded] = useState(true)

  const instA = configA.instance
  const instB = configB.instance
  const sysA = configA.system
  const sysB = configB.system

  const hasDifferences = instA.image !== instB.image
    || instA.client !== instB.client
    || instA.rollback_strategy !== instB.rollback_strategy
    || JSON.stringify(instA.command) !== JSON.stringify(instB.command)
    || JSON.stringify(instA.environment) !== JSON.stringify(instB.environment)
    || sysA.hostname !== sysB.hostname
    || sysA.cpu_model !== sysB.cpu_model
    || sysA.cpu_cores !== sysB.cpu_cores
    || sysA.memory_total_gb !== sysB.memory_total_gb

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full cursor-pointer items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 text-left hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-700/50"
      >
        <h3 className="flex items-center gap-2 text-sm/6 font-medium text-gray-900 dark:text-gray-100">
          <Settings className="size-4 text-gray-400 dark:text-gray-500" />
          Configuration Diff
          {hasDifferences && (
            <span className="rounded-xs bg-yellow-100 px-1.5 py-0.5 text-xs font-medium text-yellow-800 dark:bg-yellow-900/50 dark:text-yellow-300">
              differs
            </span>
          )}
        </h3>
        <ChevronDown className={clsx('size-5 shrink-0 text-gray-500 transition-transform', expanded && 'rotate-180')} />
      </button>
      {expanded && (
        <div className="overflow-x-auto">
          <table className="min-w-full">
            <thead>
              <tr className="border-b border-gray-200 dark:border-gray-700">
                <th className="px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-gray-500 dark:text-gray-400">Field</th>
                <th className="px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-blue-600 dark:text-blue-400">Run A</th>
                <th className="px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider text-amber-600 dark:text-amber-400">Run B</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
              {/* Instance */}
              <tr><td colSpan={3} className="px-3 pt-3 pb-1 text-xs/5 font-semibold uppercase tracking-wider text-gray-900 dark:text-gray-100">Instance</td></tr>
              <DiffRow label="Client" valueA={instA.client} valueB={instB.client} />
              <DiffRow label="Image" valueA={instA.image} valueB={instB.image} />
              {(instA.image_sha256 || instB.image_sha256) && (
                <DiffRow label="Image SHA256" valueA={instA.image_sha256 ?? ''} valueB={instB.image_sha256 ?? ''} />
              )}
              {(instA.client_version || instB.client_version) && (
                <DiffRow label="Client Version" valueA={instA.client_version ?? ''} valueB={instB.client_version ?? ''} />
              )}
              {(instA.container_runtime || instB.container_runtime) && (
                <DiffRow label="Container Runtime" valueA={instA.container_runtime ?? ''} valueB={instB.container_runtime ?? ''} />
              )}
              {(instA.entrypoint || instB.entrypoint) && (
                <DiffRow label="Entrypoint" valueA={instA.entrypoint?.join(' ') ?? ''} valueB={instB.entrypoint?.join(' ') ?? ''} />
              )}
              {(instA.command || instB.command) && (
                <DiffRow label="Command" valueA={instA.command?.join(' ') ?? ''} valueB={instB.command?.join(' ') ?? ''} />
              )}
              {(instA.extra_args || instB.extra_args) && (
                <DiffRow label="Extra Args" valueA={instA.extra_args?.join(' ') ?? ''} valueB={instB.extra_args?.join(' ') ?? ''} />
              )}
              {(instA.rollback_strategy || instB.rollback_strategy) && (
                <DiffRow label="Rollback Strategy" valueA={instA.rollback_strategy ?? 'none'} valueB={instB.rollback_strategy ?? 'none'} />
              )}
              {(instA.environment || instB.environment) && (
                <DiffRow
                  label="Environment"
                  valueA={instA.environment ? Object.entries(instA.environment).map(([k, v]) => `${k}=${v}`).join(', ') : ''}
                  valueB={instB.environment ? Object.entries(instB.environment).map(([k, v]) => `${k}=${v}`).join(', ') : ''}
                />
              )}
              {(instA.datadir || instB.datadir) && (
                <>
                  <DiffRow label="Data Dir Source" valueA={instA.datadir?.source_dir ?? ''} valueB={instB.datadir?.source_dir ?? ''} />
                  <DiffRow label="Data Dir Method" valueA={instA.datadir?.method ?? ''} valueB={instB.datadir?.method ?? ''} />
                </>
              )}

              {/* System Info */}
              <tr><td colSpan={3} className="px-3 pt-4 pb-1 text-xs/5 font-semibold uppercase tracking-wider text-gray-900 dark:text-gray-100">System</td></tr>
              <DiffRow label="Hostname" valueA={sysA.hostname} valueB={sysB.hostname} />
              <DiffRow label="OS" valueA={`${sysA.platform} ${sysA.platform_version}`} valueB={`${sysB.platform} ${sysB.platform_version}`} />
              <DiffRow label="Kernel" valueA={sysA.kernel_version} valueB={sysB.kernel_version} />
              <DiffRow label="Arch" valueA={sysA.arch} valueB={sysB.arch} />
              <DiffRow label="CPU Model" valueA={sysA.cpu_model} valueB={sysB.cpu_model} />
              <DiffRow label="CPU Cores" valueA={String(sysA.cpu_cores)} valueB={String(sysB.cpu_cores)} />
              <DiffRow label="CPU MHz" valueA={sysA.cpu_mhz.toFixed(0)} valueB={sysB.cpu_mhz.toFixed(0)} />
              <DiffRow label="Memory" valueA={`${sysA.memory_total_gb.toFixed(1)} GB`} valueB={`${sysB.memory_total_gb.toFixed(1)} GB`} />

              {/* Resource Limits */}
              {(instA.resource_limits || instB.resource_limits) && (
                <>
                  <tr><td colSpan={3} className="px-3 pt-4 pb-1 text-xs/5 font-semibold uppercase tracking-wider text-gray-900 dark:text-gray-100">Resource Limits</td></tr>
                  <DiffRow
                    label="CPU Pinning"
                    valueA={instA.resource_limits?.cpuset_cpus ?? ''}
                    valueB={instB.resource_limits?.cpuset_cpus ?? ''}
                  />
                  <DiffRow
                    label="Memory Limit"
                    valueA={instA.resource_limits?.memory_bytes ? formatBytes(instA.resource_limits.memory_bytes) : (instA.resource_limits?.memory ?? '')}
                    valueB={instB.resource_limits?.memory_bytes ? formatBytes(instB.resource_limits.memory_bytes) : (instB.resource_limits?.memory ?? '')}
                  />
                  {(instA.resource_limits?.cpu_freq_khz || instB.resource_limits?.cpu_freq_khz) && (
                    <DiffRow
                      label="CPU Frequency"
                      valueA={instA.resource_limits?.cpu_freq_khz ? formatFrequency(instA.resource_limits.cpu_freq_khz) : ''}
                      valueB={instB.resource_limits?.cpu_freq_khz ? formatFrequency(instB.resource_limits.cpu_freq_khz) : ''}
                    />
                  )}
                </>
              )}

              {/* Start Block */}
              {(configA.start_block || configB.start_block) && (
                <>
                  <tr><td colSpan={3} className="px-3 pt-4 pb-1 text-xs/5 font-semibold uppercase tracking-wider text-gray-900 dark:text-gray-100">Start Block</td></tr>
                  <DiffRow
                    label="Number"
                    valueA={configA.start_block?.number?.toLocaleString() ?? ''}
                    valueB={configB.start_block?.number?.toLocaleString() ?? ''}
                  />
                  <DiffRow
                    label="Hash"
                    valueA={configA.start_block?.hash ?? ''}
                    valueB={configB.start_block?.hash ?? ''}
                  />
                </>
              )}
            </tbody>
          </table>
        </div>
      )}
    </div>
  )
}
