export interface StorageConfig {
  s3: {
    enabled: boolean
    discovery_paths: string[]
  }
  local?: {
    enabled: boolean
    discovery_paths: string[]
  }
}

export interface RuntimeConfig {
  dataSource: string
  title?: string
  refreshInterval?: number
  api?: { baseUrl: string }
  storage?: StorageConfig
}

let cachedConfig: RuntimeConfig | null = null

export async function loadRuntimeConfig(): Promise<RuntimeConfig> {
  if (cachedConfig) return cachedConfig

  try {
    const response = await fetch('/config.json')
    if (!response.ok) {
      return { dataSource: '/results' }
    }
    const config: RuntimeConfig = await response.json()

    // If an API base URL is configured, fetch the storage config
    if (config.api?.baseUrl) {
      try {
        const configResp = await fetch(`${config.api.baseUrl}/api/v1/config`, {
          credentials: 'include',
        })
        if (configResp.ok) {
          const apiConfig = await configResp.json()
          if (apiConfig.storage) {
            config.storage = apiConfig.storage
          }
        }
      } catch {
        // API config fetch failed, continue without storage config
      }
    }

    cachedConfig = config
    return cachedConfig
  } catch {
    return { dataSource: '/results' }
  }
}

export function isS3Mode(config: RuntimeConfig): boolean {
  return config.storage?.s3?.enabled === true
}

export function isLocalMode(config: RuntimeConfig): boolean {
  return config.storage?.local?.enabled === true
}

// Maps runId/suiteHash → discovery path for S3 routing
const discoveryPathMap = new Map<string, string>()

export function registerDiscoveryMapping(key: string, discoveryPath: string): void {
  discoveryPathMap.set(key, discoveryPath)
}

export function getDiscoveryPath(key: string, config: RuntimeConfig): string {
  const mapped = discoveryPathMap.get(key)
  if (mapped) return mapped
  // Fall back to first S3 discovery path
  return config.storage?.s3?.discovery_paths?.[0] ?? 'results'
}

export function getDataUrl(path: string, config: RuntimeConfig): string {
  // Local mode: the server searches its discovery roots internally,
  // so the UI only sends the relative file path.
  if (isLocalMode(config) && config.api?.baseUrl) {
    return `${config.api.baseUrl}/api/v1/files/${path}`
  }

  if (isS3Mode(config) && config.api?.baseUrl) {
    // Extract run ID or suite hash from the path to look up the S3 discovery path
    const runMatch = path.match(/^runs\/([^/]+)/)
    const suiteMatch = path.match(/^suites\/([^/]+)/)
    const key = runMatch?.[1] ?? suiteMatch?.[1]
    const dp = key ? getDiscoveryPath(key, config) : getDiscoveryPath('', config)
    return `${config.api.baseUrl}/api/v1/files/${dp}/${path}`
  }

  const base = config.dataSource.endsWith('/')
    ? config.dataSource.slice(0, -1)
    : config.dataSource
  return `${base}/${path}`
}

export function toAbsoluteUrl(url: string): string {
  if (url.startsWith('http://') || url.startsWith('https://')) return url
  return `${window.location.origin}${url.startsWith('/') ? '' : '/'}${url}`
}
