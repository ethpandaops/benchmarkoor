import { useState } from 'react'
import clsx from 'clsx'
import { Settings, ChevronDown } from 'lucide-react'
import type { InstanceConfig } from '@/api/types'
import { cpuLayoutSummary } from '@/utils/cpuTopology'
import {
  formatApproxBandwidth,
  formatApproxCount,
  formatBytes,
  formatDiskSize,
  formatFrequency,
  formatNumber,
  withinTolerance,
} from '@/utils/format'
import { type CompareRun, type LabelMode, RUN_SLOTS, formatRunLabel } from './constants'

interface ConfigDiffProps {
  runs: CompareRun[]
  labelMode: LabelMode
}

// same overrides the text comparison, for measured values that change a
// little on each run.
function DiffRow({ label, values, same }: { label: string; values: string[]; same?: boolean }) {
  const allSame = same ?? values.every((v) => v === values[0])
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

// diskLimitsKey joins the disk limit fields, to compare them between runs.
function diskLimitsKey(inst: InstanceConfig): string {
  const rl = inst.resource_limits
  return [rl?.device_path, rl?.device_read_iops, rl?.device_write_iops, rl?.device_read_bps, rl?.device_write_bps].join('|')
}

export function ConfigDiff({ runs, labelMode }: ConfigDiffProps) {
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
      || JSON.stringify(inst.retry_new_payloads_syncing_state) !== JSON.stringify(first.retry_new_payloads_syncing_state)
      || JSON.stringify(inst.retry_new_payloads_failed_state) !== JSON.stringify(first.retry_new_payloads_failed_state)
      || JSON.stringify(inst.command) !== JSON.stringify(first.command)
      || JSON.stringify(inst.environment) !== JSON.stringify(first.environment)
      || diskLimitsKey(inst) !== diskLimitsKey(first),
    ) || systems.some((sys) =>
      sys.hostname !== firstSys.hostname
      || sys.cpu_model !== firstSys.cpu_model
      || sys.cpu_cores !== firstSys.cpu_cores
      || sys.memory_total_gb !== firstSys.memory_total_gb
      || sys.storage?.device?.path !== firstSys.storage?.device?.path
      || sys.storage?.filesystem !== firstSys.storage?.filesystem,
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
                      <span title={formatRunLabel(slot, run, labelMode)}>Run {slot.label}</span>
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
              {instances.some((i) => i.retry_new_payloads_syncing_state) && (
                <DiffRow
                  label="Retry on SYNCING"
                  values={instances.map((i) => {
                    const r = i.retry_new_payloads_syncing_state
                    if (!r || !r.enabled) return 'disabled'
                    return `enabled (max=${r.max_retries}, backoff=${r.backoff})`
                  })}
                />
              )}
              {instances.some((i) => i.retry_new_payloads_failed_state) && (
                <DiffRow
                  label="Retry on Failure"
                  values={instances.map((i) => {
                    const r = i.retry_new_payloads_failed_state
                    if (!r || !r.enabled) return 'disabled'
                    return `enabled (max=${r.max_retries}, backoff=${r.backoff})`
                  })}
                />
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
              {systems.some((s) => s.cpu_threads !== undefined) && (
                <DiffRow label="CPU Threads" values={systems.map((s) => s.cpu_threads !== undefined ? String(s.cpu_threads) : '')} />
              )}
              <DiffRow label="CPU MHz" values={systems.map((s) => s.cpu_mhz.toFixed(0))} />
              <DiffRow label="Memory" values={systems.map((s) => `${s.memory_total_gb.toFixed(1)} GB`)} />
              {systems.some((s) => s.storage?.device) && (
                <DiffRow
                  label="Storage Device"
                  values={systems.map((s) => {
                    const d = s.storage?.device
                    return d ? [d.path, d.kind, d.model].filter(Boolean).join(' · ') : ''
                  })}
                />
              )}
              {systems.some((s) => s.storage?.device?.size_bytes) && (
                <DiffRow
                  label="Storage Size"
                  values={systems.map((s) => {
                    const size = s.storage?.device?.size_bytes
                    return size ? formatDiskSize(size) : ''
                  })}
                />
              )}
              {systems.some((s) => s.storage?.filesystem) && (
                <DiffRow
                  label="Filesystem"
                  values={systems.map((s) => {
                    const st = s.storage
                    if (!st?.filesystem) return ''
                    return st.partition ? `${st.filesystem} on ${st.partition}` : st.filesystem
                  })}
                />
              )}
              {/* A probe row shows a difference only above the measurement tolerance. */}
              {systems.some((s) => s.storage?.probe) && (
                <>
                  <DiffRow
                    label="Disk Random IOPS (R/W)"
                    values={systems.map((s) => {
                      const p = s.storage?.probe
                      return p ? `${formatApproxCount(p.rand_read_iops)} / ${formatApproxCount(p.rand_write_iops)}` : ''
                    })}
                    same={
                      withinTolerance(systems.map((s) => s.storage?.probe?.rand_read_iops))
                      && withinTolerance(systems.map((s) => s.storage?.probe?.rand_write_iops))
                    }
                  />
                  <DiffRow
                    label="Disk Sequential (R/W)"
                    values={systems.map((s) => {
                      const p = s.storage?.probe
                      return p ? `${formatApproxBandwidth(p.seq_read_bps)} / ${formatApproxBandwidth(p.seq_write_bps)}` : ''
                    })}
                    same={
                      withinTolerance(systems.map((s) => s.storage?.probe?.seq_read_bps))
                      && withinTolerance(systems.map((s) => s.storage?.probe?.seq_write_bps))
                    }
                  />
                </>
              )}

              {/* Resource Limits */}
              {instances.some((i) => i.resource_limits) && (
                <>
                  <tr><td colSpan={colCount} className="px-3 pt-4 pb-1 text-xs/5 font-semibold uppercase tracking-wider text-gray-900 dark:text-gray-100">Resource Limits</td></tr>
                  <DiffRow
                    label="CPU Pinning"
                    values={instances.map((i) => i.resource_limits?.cpuset_cpus ?? '')}
                  />
                  {instances.some((i) => i.resource_limits?.cpuset_topology) && (
                    <DiffRow
                      label="CPU Pinning Topology"
                      values={instances.map((i) => i.resource_limits?.cpuset_topology ?? '')}
                    />
                  )}
                  {runs.some((r) => r.config.system.cpu_topology && r.config.instance.resource_limits?.cpuset_cpus) && (
                    <DiffRow
                      label="CPU Layout"
                      values={runs.map((r) => cpuLayoutSummary(r.config.system.cpu_topology, r.config.instance.resource_limits?.cpuset_cpus))}
                    />
                  )}
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
                  {instances.some((i) => i.resource_limits?.device_path) && (
                    <DiffRow
                      label="Disk Limit Device"
                      values={instances.map((i) => i.resource_limits?.device_path ?? '')}
                    />
                  )}
                  {instances.some((i) => i.resource_limits?.device_read_iops || i.resource_limits?.device_write_iops) && (
                    <DiffRow
                      label="Disk IOPS Limit (R/W)"
                      values={instances.map((i) => {
                        const rl = i.resource_limits
                        if (!rl?.device_read_iops && !rl?.device_write_iops) return ''
                        const fmt = (v?: number) => (v ? formatNumber(v) : 'none')
                        return `${fmt(rl.device_read_iops)} / ${fmt(rl.device_write_iops)}`
                      })}
                    />
                  )}
                  {instances.some((i) => i.resource_limits?.device_read_bps || i.resource_limits?.device_write_bps) && (
                    <DiffRow
                      label="Disk Bandwidth Limit (R/W)"
                      values={instances.map((i) => {
                        const rl = i.resource_limits
                        if (!rl?.device_read_bps && !rl?.device_write_bps) return ''
                        const fmt = (v?: number) => (v ? `${formatBytes(v)}/s` : 'none')
                        return `${fmt(rl.device_read_bps)} / ${fmt(rl.device_write_bps)}`
                      })}
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
