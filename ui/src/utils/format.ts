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

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`
}

export function formatFrequency(kHz: number): string {
  if (kHz >= 1_000_000) return `${(kHz / 1_000_000).toFixed(2)} GHz`
  if (kHz >= 1_000) return `${(kHz / 1_000).toFixed(0)} MHz`
  return `${kHz} kHz`
}
