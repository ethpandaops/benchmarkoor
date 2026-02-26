import { useState } from 'react'
import clsx from 'clsx'
import { Settings, ChevronDown } from 'lucide-react'
import { formatBytes, formatFrequency } from '@/utils/format'
import { type CompareRun, RUN_SLOTS } from './constants'

interface ConfigDiffProps {
  runs: CompareRun[]
}

function DiffRow({ label, values }: { label: string; values: string[] }) {
  const allSame = values.every((v) => v === values[0])
  return (
    <tr className={clsx(!allSame && 'bg-yellow-50/50 dark:bg-yellow-900/10')}>
      <td className="px-3 py-1.5 text-xs/5 font-medium text-gray-500 dark:text-gray-400">{label}</td>
      {values.map((val, i) => {
        const slot = RUN_SLOTS[i]
        return (
          <td key={slot.label} className={clsx('px-3 py-1.5 font-mono text-xs/5', !allSame ? `font-medium ${slot.diffTextClass}` : 'text-gray-700 dark:text-gray-300')}>
            {val || '-'}
          </td>
        )
      })}
    </tr>
  )
}

export function ConfigDiff({ runs }: ConfigDiffProps) {
  const [expanded, setExpanded] = useState(true)

  const instances = runs.map((r) => r.config.instance)
  const systems = runs.map((r) => r.config.system)
  const colCount = runs.length + 1

  const hasDifferences = (() => {
    const first = instances[0]
    const firstSys = systems[0]
    return instances.some((inst) =>
      inst.image !== first.image
      || inst.client !== first.client
      || inst.rollback_strategy !== first.rollback_strategy
      || JSON.stringify(inst.command) !== JSON.stringify(first.command)
      || JSON.stringify(inst.environment) !== JSON.stringify(first.environment),
    ) || systems.some((sys) =>
      sys.hostname !== firstSys.hostname
      || sys.cpu_model !== firstSys.cpu_model
      || sys.cpu_cores !== firstSys.cpu_cores
      || sys.memory_total_gb !== firstSys.memory_total_gb,
    )
  })()

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
                {runs.map((run) => {
                  const slot = RUN_SLOTS[run.index]
                  return (
                    <th key={slot.label} className={clsx('px-3 py-2 text-left text-xs/5 font-medium uppercase tracking-wider', slot.textClass, `dark:${slot.textDarkClass.replace('text-', 'text-')}`)}>
                      Run {slot.label}
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
              {/* Instance */}
              <tr><td colSpan={colCount} className="px-3 pt-3 pb-1 text-xs/5 font-semibold uppercase tracking-wider text-gray-900 dark:text-gray-100">Instance</td></tr>
              <DiffRow label="Client" values={instances.map((i) => i.client)} />
              <DiffRow label="Image" values={instances.map((i) => i.image)} />
              {instances.some((i) => i.image_sha256) && (
                <DiffRow label="Image SHA256" values={instances.map((i) => i.image_sha256 ?? '')} />
              )}
              {instances.some((i) => i.client_version) && (
                <DiffRow label="Client Version" values={instances.map((i) => i.client_version ?? '')} />
              )}
              {instances.some((i) => i.container_runtime) && (
                <DiffRow label="Container Runtime" values={instances.map((i) => i.container_runtime ?? '')} />
              )}
              {instances.some((i) => i.entrypoint) && (
                <DiffRow label="Entrypoint" values={instances.map((i) => i.entrypoint?.join(' ') ?? '')} />
              )}
              {instances.some((i) => i.command) && (
                <DiffRow label="Command" values={instances.map((i) => i.command?.join(' ') ?? '')} />
              )}
              {instances.some((i) => i.extra_args) && (
                <DiffRow label="Extra Args" values={instances.map((i) => i.extra_args?.join(' ') ?? '')} />
              )}
              {instances.some((i) => i.rollback_strategy) && (
                <DiffRow label="Rollback Strategy" values={instances.map((i) => i.rollback_strategy ?? 'none')} />
              )}
              {instances.some((i) => i.environment) && (
                <DiffRow
                  label="Environment"
                  values={instances.map((i) => i.environment ? Object.entries(i.environment).map(([k, v]) => `${k}=${v}`).join(', ') : '')}
                />
              )}
              {instances.some((i) => i.datadir) && (
                <>
                  <DiffRow label="Data Dir Source" values={instances.map((i) => i.datadir?.source_dir ?? '')} />
                  <DiffRow label="Data Dir Method" values={instances.map((i) => i.datadir?.method ?? '')} />
                </>
              )}

              {/* System Info */}
              <tr><td colSpan={colCount} className="px-3 pt-4 pb-1 text-xs/5 font-semibold uppercase tracking-wider text-gray-900 dark:text-gray-100">System</td></tr>
              <DiffRow label="Hostname" values={systems.map((s) => s.hostname)} />
              <DiffRow label="OS" values={systems.map((s) => `${s.platform} ${s.platform_version}`)} />
              <DiffRow label="Kernel" values={systems.map((s) => s.kernel_version)} />
              <DiffRow label="Arch" values={systems.map((s) => s.arch)} />
              <DiffRow label="CPU Model" values={systems.map((s) => s.cpu_model)} />
              <DiffRow label="CPU Cores" values={systems.map((s) => String(s.cpu_cores))} />
              <DiffRow label="CPU MHz" values={systems.map((s) => s.cpu_mhz.toFixed(0))} />
              <DiffRow label="Memory" values={systems.map((s) => `${s.memory_total_gb.toFixed(1)} GB`)} />

              {/* Resource Limits */}
              {instances.some((i) => i.resource_limits) && (
                <>
                  <tr><td colSpan={colCount} className="px-3 pt-4 pb-1 text-xs/5 font-semibold uppercase tracking-wider text-gray-900 dark:text-gray-100">Resource Limits</td></tr>
                  <DiffRow
                    label="CPU Pinning"
                    values={instances.map((i) => i.resource_limits?.cpuset_cpus ?? '')}
                  />
                  <DiffRow
                    label="Memory Limit"
                    values={instances.map((i) => i.resource_limits?.memory_bytes ? formatBytes(i.resource_limits.memory_bytes) : (i.resource_limits?.memory ?? ''))}
                  />
                  {instances.some((i) => i.resource_limits?.cpu_freq_khz) && (
                    <DiffRow
                      label="CPU Frequency"
                      values={instances.map((i) => i.resource_limits?.cpu_freq_khz ? formatFrequency(i.resource_limits.cpu_freq_khz) : '')}
                    />
                  )}
                </>
              )}

              {/* Start Block */}
              {runs.some((r) => r.config.start_block) && (
                <>
                  <tr><td colSpan={colCount} className="px-3 pt-4 pb-1 text-xs/5 font-semibold uppercase tracking-wider text-gray-900 dark:text-gray-100">Start Block</td></tr>
                  <DiffRow
                    label="Number"
                    values={runs.map((r) => r.config.start_block?.number?.toLocaleString() ?? '')}
                  />
                  <DiffRow
                    label="Hash"
                    values={runs.map((r) => r.config.start_block?.hash ?? '')}
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
