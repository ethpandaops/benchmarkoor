import clsx from 'clsx'
import type { BlockDevice, ResourceLimitsConfig, StorageInfo, StorageLatency } from '@/api/types'
import { formatBytes, formatDiskSize, formatNumber } from '@/utils/format'

interface StorageInfoPanelProps {
  storage?: StorageInfo
  limits?: ResourceLimitsConfig
}

// The storage recommendations of EIP-7870 (hardware for an Ethereum node).
// "~500 MB/s" is a decimal megabyte.
const EIP_7870_URL = 'https://eips.ethereum.org/EIPS/eip-7870'

interface WorkloadRow {
  label: string
  measured?: number
  limit?: number
  recommended: number
  /** The recommendation as EIP-7870 writes it. */
  recommendedLabel: string
  format: (value: number) => string
}

function formatIOPS(value: number): string {
  return formatNumber(Math.round(value))
}

function formatBandwidth(value: number): string {
  return `${formatBytes(value)}/s`
}

function formatLatency(us: number): string {
  return us >= 1000 ? `${(us / 1000).toFixed(1)} ms` : `${Math.round(us)} µs`
}

/** The label suffix of a mixed workload, e.g. " (75/25 mix)". */
function mixLabel(readPercent?: number): string {
  return readPercent ? ` (${readPercent}/${100 - readPercent} mix)` : ''
}

// Runs from before the "ramdisk" kind have "other" for a brd RAM disk.
function isRamDisk(device: BlockDevice): boolean {
  return device.kind === 'ramdisk' || /^ram\d+$/.test(device.name)
}

function deviceType(device: BlockDevice): string {
  if (isRamDisk(device)) return 'RAM disk'
  const media = device.rotational === undefined ? '' : device.rotational ? 'HDD' : 'SSD'
  switch (device.kind) {
    case 'nvme':
      return 'NVMe SSD'
    case 'device-mapper':
      return device.dm_name ? `Device mapper (${device.dm_name})` : 'Device mapper'
    case 'md':
      return 'Software RAID (md)'
    case 'scsi':
      return media ? `SATA/SAS ${media}` : 'SATA/SAS'
    case 'virtio':
    case 'xen':
      return `Virtual disk (${device.kind})`
    default:
      return media || device.kind || '-'
  }
}

function deviceLabel(device: BlockDevice): string {
  const parts = [device.vendor, device.model].filter(Boolean).join(' ')
  const size = device.size_bytes ? formatDiskSize(device.size_bytes) : ''
  return [device.name, parts, size].filter(Boolean).join(' · ')
}

function Item({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div>
      <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">{label}</dt>
      <dd className={clsx('mt-1 text-sm/6 break-all text-gray-900 dark:text-gray-100', mono && 'font-mono')}>{value}</dd>
    </div>
  )
}

export function StorageInfoPanel({ storage, limits }: StorageInfoPanelProps) {
  const device = storage?.device
  const probe = storage?.probe
  const hasLimits = Boolean(
    limits?.device_read_iops || limits?.device_write_iops || limits?.device_read_bps || limits?.device_write_bps,
  )

  if (!storage && !hasLimits) return null

  const qd1Rows = [
    { label: 'Random read (4 KiB)', latency: probe?.qd1_rand_read },
    { label: 'Random write (4 KiB)', latency: probe?.qd1_rand_write },
  ].filter((row): row is { label: string; latency: StorageLatency } => row.latency !== undefined)

  const rows: WorkloadRow[] = [
    {
      label: `Random read IOPS (4 KiB${mixLabel(probe?.rand_read_percent)})`,
      measured: probe?.rand_read_iops,
      limit: limits?.device_read_iops,
      recommended: 50_000,
      recommendedLabel: '50,000',
      format: formatIOPS,
    },
    {
      label: `Random write IOPS (4 KiB${mixLabel(probe?.rand_read_percent)})`,
      measured: probe?.rand_write_iops,
      limit: limits?.device_write_iops,
      recommended: 15_000,
      recommendedLabel: '15,000',
      format: formatIOPS,
    },
    {
      label: `Sequential read (1 MiB${mixLabel(probe?.seq_read_percent)})`,
      measured: probe?.seq_read_bps,
      limit: limits?.device_read_bps,
      recommended: 500e6,
      recommendedLabel: '500 MB/s',
      format: formatBandwidth,
    },
    {
      label: `Sequential write (1 MiB${mixLabel(probe?.seq_read_percent)})`,
      measured: probe?.seq_write_bps,
      limit: limits?.device_write_bps,
      recommended: 500e6,
      recommendedLabel: '500 MB/s',
      format: formatBandwidth,
    },
  ]

  return (
    <div className="mt-6 border-t border-gray-200 pt-6 dark:border-gray-700">
      <h4 className="mb-3 text-sm/6 font-medium text-gray-900 dark:text-gray-100">Storage</h4>

      {device && (
        <dl className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <Item label="Device" value={device.path} mono />
          <Item label="Type" value={deviceType(device)} />
          {device.model && <Item label="Model" value={[device.vendor, device.model].filter(Boolean).join(' ')} />}
          {device.size_bytes !== undefined && <Item label="Size" value={formatDiskSize(device.size_bytes)} />}
          {storage?.filesystem && (
            <Item
              label="Filesystem"
              value={storage.partition ? `${storage.filesystem} on ${storage.partition}` : storage.filesystem}
            />
          )}
          {device.scheduler && <Item label="I/O Scheduler" value={device.scheduler} />}
          {device.logical_block_size !== undefined && (
            <Item
              label="Block Size"
              value={`${device.logical_block_size} B logical / ${device.physical_block_size ?? '-'} B physical`}
            />
          )}
          {device.firmware && <Item label="Firmware" value={device.firmware} />}
          {storage?.data_path && (
            <div className="sm:col-span-2 lg:col-span-3">
              <Item label="Data Path" value={storage.data_path} mono />
            </div>
          )}
          {device.backing && device.backing.length > 0 && (
            <div className="sm:col-span-2 lg:col-span-3">
              <dt className="text-xs/5 font-medium text-gray-500 dark:text-gray-400">Backing Disks</dt>
              <dd className="mt-1 flex flex-col gap-1 text-sm/6 text-gray-900 dark:text-gray-100">
                {device.backing.map((disk) => (
                  <span key={disk.name}>
                    {deviceLabel(disk)} <span className="text-gray-500 dark:text-gray-400">({deviceType(disk)})</span>
                  </span>
                ))}
              </dd>
              {device.backing.some(isRamDisk) && (
                <p className="mt-1 text-xs/5 text-gray-500 dark:text-gray-400">
                  The RAM disk holds the device mapper metadata, such as the dm-era metadata of schelk. It does not hold
                  the data, so the probe does not measure it.
                </p>
              )}
            </div>
          )}
        </dl>
      )}

      {storage?.error && (
        <p className="text-sm/6 text-amber-700 dark:text-amber-400">Block device unknown: {storage.error}</p>
      )}

      {(probe || hasLimits) && (
        <div className="mt-4 overflow-x-auto">
          <table className="min-w-full text-sm/6 whitespace-nowrap">
            <thead>
              <tr className="border-b border-gray-200 text-left text-xs/5 font-medium text-gray-500 dark:border-gray-700 dark:text-gray-400">
                <th className="py-2 pr-4 font-medium">Workload</th>
                <th className="py-2 pr-4 font-medium" title="Measured on the host, outside any container">
                  Host capacity
                </th>
                <th className="py-2 pr-4 font-medium" title="The resource_limits.device_* throttle on the client container">
                  Container limit
                </th>
                <th className="py-2 pr-4 font-medium" title="The lower of the host capacity and the container limit">
                  Effective
                </th>
                <th className="py-2 font-medium">
                  <a href={EIP_7870_URL} target="_blank" rel="noreferrer" className="hover:underline">
                    EIP-7870
                  </a>
                </th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
              {rows.map((row) => {
                const known = [row.measured, row.limit].filter((v): v is number => v !== undefined && v > 0)
                const effective = known.length > 0 ? Math.min(...known) : undefined
                const meets = effective !== undefined && effective >= row.recommended
                return (
                  <tr key={row.label} className="text-gray-900 dark:text-gray-100">
                    <td className="py-2 pr-4 text-gray-500 dark:text-gray-400">{row.label}</td>
                    <td className="py-2 pr-4 font-mono">{row.measured ? row.format(row.measured) : '-'}</td>
                    <td className="py-2 pr-4 font-mono">{row.limit ? row.format(row.limit) : 'none'}</td>
                    <td
                      className={clsx(
                        'py-2 pr-4 font-mono',
                        effective !== undefined && meets && 'text-green-700 dark:text-green-400',
                        effective !== undefined && !meets && 'text-amber-700 dark:text-amber-400',
                      )}
                      title={
                        effective === undefined
                          ? undefined
                          : meets
                            ? 'Meets the EIP-7870 recommendation'
                            : 'Below the EIP-7870 recommendation'
                      }
                    >
                      {effective !== undefined ? row.format(effective) : '-'}
                    </td>
                    <td className="py-2 font-mono text-gray-500 dark:text-gray-400">≥ {row.recommendedLabel}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {qd1Rows.length > 0 && (
        <div className="mt-4 overflow-x-auto">
          <table className="min-w-full text-sm/6 whitespace-nowrap">
            <thead>
              <tr className="border-b border-gray-200 text-left text-xs/5 font-medium text-gray-500 dark:border-gray-700 dark:text-gray-400">
                <th className="py-2 pr-4 font-medium">Workload at QD1</th>
                <th className="py-2 pr-4 font-medium" title="Measured on the host, with one I/O in flight">
                  IOPS
                </th>
                <th className="py-2 pr-4 font-medium">p50 latency</th>
                <th className="py-2 font-medium">p99 latency</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-gray-100 dark:divide-gray-700/50">
              {qd1Rows.map(({ label, latency }) => (
                <tr key={label} className="text-gray-900 dark:text-gray-100">
                  <td className="py-2 pr-4 text-gray-500 dark:text-gray-400">{label}</td>
                  <td className="py-2 pr-4 font-mono">{formatIOPS(latency.iops)}</td>
                  <td className="py-2 pr-4 font-mono">{formatLatency(latency.p50_us)}</td>
                  <td className="py-2 font-mono">{formatLatency(latency.p99_us)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <div className="mt-2 flex flex-col gap-1 text-xs/5 text-gray-500 dark:text-gray-400">
        {probe && probe.rand_read_percent !== undefined && (
          <p>
            Host capacity: {formatBytes(probe.file_size_bytes)} of direct I/O for each row pair, at I/O depth{' '}
            {probe.io_depth} from one thread with native AIO, as in the EIP-7870 fio commands. The read and write values
            of each row pair come from one mixed run.
          </p>
        )}
        {probe && probe.rand_read_percent === undefined && (
          <p>
            Host capacity: direct I/O on a {formatBytes(probe.file_size_bytes)} file at I/O depth {probe.io_depth},{' '}
            {probe.duration} per workload. Separate read and write runs (older probe), so the values are higher than in
            the EIP-7870 mixed runs.
          </p>
        )}
        {qd1Rows.length > 0 && (
          <p>
            QD1: one I/O in flight for {probe?.duration} each, as when a client reads state one key at a time. Writes do
            not use fsync.
          </p>
        )}
        {!probe && storage?.probe_error && <p>Disk probe failed: {storage.probe_error}</p>}
        {!probe && !storage?.probe_error && storage?.device && (
          <p>Host capacity was not measured. Set runner.storage_probe.enabled to measure it.</p>
        )}
        {hasLimits && limits?.device_path && limits.device_throttle !== 'io.cost' && (
          <p>The container limits throttle {limits.device_path}.</p>
        )}
        {hasLimits && limits?.device_path && limits.device_throttle === 'io.cost' && (
          <p>
            An io.cost model throttles all I/O on {limits.device_path}. Reads and writes share one budget, and a direction
            without a limit uses almost none of it.
          </p>
        )}
      </div>
    </div>
  )
}
