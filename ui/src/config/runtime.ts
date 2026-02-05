export interface RuntimeConfig {
  dataSource: string
  title?: string
  refreshInterval?: number
}

let cachedConfig: RuntimeConfig | null = null

export async function loadRuntimeConfig(): Promise<RuntimeConfig> {
  if (cachedConfig) return cachedConfig

  try {
    const response = await fetch('/config.json')
    if (!response.ok) {
      return { dataSource: '/results' }
    }
    cachedConfig = await response.json()
    return cachedConfig!
  } catch {
    return { dataSource: '/results' }
  }
}

export function getDataUrl(path: string, config: RuntimeConfig): string {
  const base = config.dataSource.endsWith('/')
    ? config.dataSource.slice(0, -1)
    : config.dataSource
  return `${base}/${path}`
}

export function toAbsoluteUrl(url: string): string {
  if (url.startsWith('http://') || url.startsWith('https://')) return url
  return `${window.location.origin}${url.startsWith('/') ? '' : '/'}${url}`
}
