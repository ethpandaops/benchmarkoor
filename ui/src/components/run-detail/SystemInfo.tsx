import { useState } from 'react'
import clsx from 'clsx'
import type { SystemInfo as SystemInfoType } from '@/api/types'

interface SystemInfoProps {
  system: SystemInfoType
}

function InfoItem({ label, value }: { label: string; value: string | number }) {
  return (
    <div>
      <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">{label}</dt>
      <dd className="mt-1 text-sm/6 text-gray-900 dark:text-gray-100">{value}</dd>
    </div>
  )
}

export function SystemInfo({ system }: SystemInfoProps) {
  const [expanded, setExpanded] = useState(false)

  const summary = `${system.platform} (${system.arch}) / ${system.cpu_cores} Cores / ${system.cpu_mhz.toFixed(0)} MHz / ${system.memory_total_gb.toFixed(1)} GB`

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <button
        onClick={() => setExpanded(!expanded)}
        className="flex w-full items-center justify-between gap-3 border-b border-gray-200 px-4 py-3 text-left hover:bg-gray-50 dark:border-gray-700 dark:hover:bg-gray-700/50"
      >
        <h3 className="shrink-0 text-sm/6 font-medium text-gray-900 dark:text-gray-100">System Information</h3>
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
        <div className="p-4">
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
        </div>
      )}
    </div>
  )
}
