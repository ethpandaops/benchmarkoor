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

export interface IndexingConfig {
  enabled: boolean
}

export interface RuntimeConfig {
  dataSource: string
  title?: string
  refreshInterval?: number
  api?: { baseUrl: string }
  storage?: StorageConfig
  indexing?: IndexingConfig
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
          if (apiConfig.indexing) {
            config.indexing = apiConfig.indexing
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

export function isIndexingEnabled(config: RuntimeConfig): boolean {
  return config.indexing?.enabled === true
}

// Maps runId/suiteHash → discovery path for S3 routing
const discoveryPathMap = new Map<string, string>()

export function registerDiscoveryMapping(key: string, discoveryPath: string): void {
  discoveryPathMap.set(key, discoveryPath)
}

export function getDiscoveryPath(key: string, config: RuntimeConfig): string {
  const mapped = discoveryPathMap.get(key)
  if (mapped) return mapped
  // Fall back to first available discovery path (S3 or local)
  return (
    config.storage?.s3?.discovery_paths?.[0] ??
    config.storage?.local?.discovery_paths?.[0] ??
    'results'
  )
}

export function getDataUrl(path: string, config: RuntimeConfig): string {
  // Both S3 and local mode use the same {discovery_path}/{relative_path} URL
  // pattern. The discovery path prefix is looked up from the run/suite ID.
  if ((isS3Mode(config) || isLocalMode(config)) && config.api?.baseUrl) {
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
