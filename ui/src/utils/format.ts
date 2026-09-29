export function formatDuration(nanoseconds: number): string {
  if (nanoseconds < 1000) {
    return `${nanoseconds}ns`
  }
  if (nanoseconds < 1_000_000) {
    return `${(nanoseconds / 1000).toFixed(2)}µs`
  }
  if (nanoseconds < 1_000_000_000) {
    return `${(nanoseconds / 1_000_000).toFixed(2)}ms`
  }
  const totalSeconds = Math.floor(nanoseconds / 1_000_000_000)
  if (totalSeconds < 60) {
    return `${(nanoseconds / 1_000_000_000).toFixed(2)}s`
  }
  const hours = Math.floor(totalSeconds / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  if (hours > 0) {
    return `${hours}h${minutes}m${seconds}s`
  }
  return `${minutes}m${seconds}s`
}

// formatDurationMs renders a millisecond duration in human-readable form
// (e.g. 6006337 -> "1h40m6s"), reusing the nanosecond formatter.
export function formatDurationMs(milliseconds: number): string {
  return formatDuration(milliseconds * 1_000_000)
}

export function formatNumber(num: number): string {
  return new Intl.NumberFormat().format(num)
}

export function formatBytes(bytes: number | undefined): string {
  if (bytes == null || !Number.isFinite(bytes)) return '-'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`
}

// Disk vendors give sizes in decimal units, so a 4 TB drive shows as 4 TB.
export function formatDiskSize(bytes: number): string {
  if (bytes >= 1e12) return `${(bytes / 1e12).toFixed(2)} TB`
  if (bytes >= 1e9) return `${(bytes / 1e9).toFixed(1)} GB`
  return `${(bytes / 1e6).toFixed(0)} MB`
}

const approxCount = new Intl.NumberFormat('en', { notation: 'compact', maximumSignificantDigits: 2 })

// A measured count rounded to 2 significant figures, e.g. 667803 -> "~670K".
// Two measurements of the same device then usually give the same text.
export function formatApproxCount(value: number): string {
  return `~${approxCount.format(value)}`
}

// A measured byte rate rounded to 2 significant figures in the unit of
// formatBytes, e.g. 221623890739 -> "~210 GB/s".
export function formatApproxBandwidth(bytesPerSec: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytesPerSec
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit++
  }
  return `~${Number(value.toPrecision(2))} ${units[unit]}/s`
}

// withinTolerance reports if all measured values are within tolerance of each
// other (largest / smallest - 1). The disk probe gives about 5-10% spread
// between runs on the same device, so the default is 15%. A missing value
// only matches another missing value.
export function withinTolerance(values: (number | undefined)[], tolerance = 0.15): boolean {
  const known = values.filter((v): v is number => v !== undefined)
  if (known.length === 0) return true
  if (known.length !== values.length) return false
  const min = Math.min(...known)
  const max = Math.max(...known)
  if (min <= 0) return max === min
  return max / min - 1 <= tolerance
}

export function formatFrequency(kHz: number): string {
  if (kHz >= 1_000_000) return `${(kHz / 1_000_000).toFixed(2)} GHz`
  if (kHz >= 1_000) return `${(kHz / 1_000).toFixed(0)} MHz`
  return `${kHz} kHz`
}
