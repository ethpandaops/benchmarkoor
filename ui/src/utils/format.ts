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
  return `${(nanoseconds / 1_000_000_000).toFixed(2)}s`
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
