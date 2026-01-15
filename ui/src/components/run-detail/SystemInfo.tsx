import type { SystemInfo as SystemInfoType } from '@/api/types'
import { Card } from '@/components/shared/Card'

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
  return (
    <Card title="System Information" collapsible defaultCollapsed>
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
    </Card>
  )
}
