import { loadRuntimeConfig, getDataUrl } from '@/config/runtime'

export interface FetchResult<T> {
  data: T | null
  status: number
}

// Check if the content type indicates JSON
function isJsonContentType(response: Response): boolean {
  const contentType = response.headers.get('content-type')
  return contentType?.includes('application/json') ?? false
}

export async function fetchData<T>(path: string): Promise<FetchResult<T>> {
  const config = await loadRuntimeConfig()
  const url = getDataUrl(path, config)

  const response = await fetch(url)

  if (!response.ok) {
    return { data: null, status: response.status }
  }

  // SPA servers may return 200 with HTML for missing files
  // Treat non-JSON responses as 404
  if (!isJsonContentType(response)) {
    return { data: null, status: 404 }
  }

  const data = await response.json()
  return { data, status: response.status }
}

export interface HeadResult {
  exists: boolean
  size: number | null
  url: string
}

export async function fetchHead(path: string): Promise<HeadResult> {
  const config = await loadRuntimeConfig()
  const url = getDataUrl(path, config)

  try {
    const response = await fetch(url, { method: 'HEAD' })
    if (!response.ok) {
      return { exists: false, size: null, url }
    }
    const contentLength = response.headers.get('content-length')
    return { exists: true, size: contentLength ? parseInt(contentLength, 10) : null, url }
  } catch {
    return { exists: false, size: null, url }
  }
}

export async function fetchText(path: string): Promise<FetchResult<string>> {
  const config = await loadRuntimeConfig()
  const url = getDataUrl(path, config)

  const response = await fetch(url)

  if (!response.ok) {
    return { data: null, status: response.status }
  }

  // SPA servers may return 200 with HTML for missing files
  // Check if we got HTML when expecting text data
  const data = await response.text()
  if (data.trimStart().startsWith('<!DOCTYPE') || data.trimStart().startsWith('<html')) {
    return { data: null, status: 404 }
  }

  return { data, status: response.status }
}
