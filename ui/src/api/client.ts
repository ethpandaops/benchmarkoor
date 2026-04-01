import { loadRuntimeConfig, getDataUrl, isS3Mode, isLocalMode } from '@/config/runtime'

export interface FetchResult<T> {
  data: T | null
  status: number
}

// Append a cache-busting query parameter rounded to the given interval.
// Requests within the same interval share the same cache key.
function cacheBustUrl(url: string, intervalSec = 60): string {
  const separator = url.includes('?') ? '&' : '?'
  return `${url}${separator}_t=${Math.floor(Date.now() / (intervalSec * 1000))}`
}

// Check if the content type indicates JSON
function isJsonContentType(response: Response): boolean {
  const contentType = response.headers.get('content-type')
  return contentType?.includes('application/json') ?? false
}

// Fetches a presigned URL from the API (with credentials for auth),
// then fetches the actual content from S3 (without credentials).
export async function fetchViaS3(
  url: string,
  init?: RequestInit,
  cacheBustInterval?: number,
): Promise<Response> {
  const resp = await fetch(cacheBustUrl(url, cacheBustInterval), { credentials: 'include' })
  if (!resp.ok) return resp

  const { url: presignedUrl } = await resp.json()

  return fetch(presignedUrl, { ...init, cache: 'no-cache' })
}

export async function fetchData<T>(path: string, opts?: { cacheBustInterval?: number }): Promise<FetchResult<T>> {
  const config = await loadRuntimeConfig()
  const url = getDataUrl(path, config)

  let response: Response

  if (isS3Mode(config)) {
    response = await fetchViaS3(url, undefined, opts?.cacheBustInterval)
  } else if (isLocalMode(config)) {
    response = await fetch(cacheBustUrl(url, opts?.cacheBustInterval), { credentials: 'include' })
  } else {
    response = await fetch(cacheBustUrl(url, opts?.cacheBustInterval))
  }

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

// Limits concurrent requests to avoid ERR_INSUFFICIENT_RESOURCES when many
// HEAD requests are queued at once (e.g. 1000+ files).
function createConcurrencyLimiter(max: number) {
  let active = 0
  const queue: Array<() => void> = []

  return async function <T>(fn: () => Promise<T>): Promise<T> {
    if (active >= max) {
      await new Promise<void>((resolve) => queue.push(resolve))
    }
    active++
    try {
      return await fn()
    } finally {
      active--
      queue.shift()?.()
    }
  }
}

const headLimiter = createConcurrencyLimiter(8)

export async function fetchHead(path: string): Promise<HeadResult> {
  const config = await loadRuntimeConfig()
  const url = getDataUrl(path, config)

  return headLimiter(async () => {
    try {
      if (isS3Mode(config) || isLocalMode(config)) {
        // The API handles HEAD requests for both backends: local mode uses
        // http.ServeFile, S3 mode calls HeadObject. Both return
        // Content-Length directly so the UI can read file sizes reliably.
        const response = await fetch(url, { method: 'HEAD', credentials: 'include' })
        if (!response.ok) {
          return { exists: false, size: null, url }
        }
        const contentLength = response.headers.get('content-length')
        // For S3, return the API URL with ?redirect=true so <a href>
        // triggers a 302 redirect to the presigned S3 URL.
        const downloadUrl = isS3Mode(config)
          ? `${url}${url.includes('?') ? '&' : '?'}redirect=true`
          : url
        return { exists: true, size: contentLength ? parseInt(contentLength, 10) : null, url: downloadUrl }
      }

      const response = await fetch(url, { method: 'HEAD' })
      if (!response.ok) {
        return { exists: false, size: null, url }
      }
      const contentLength = response.headers.get('content-length')
      return { exists: true, size: contentLength ? parseInt(contentLength, 10) : null, url }
    } catch {
      return { exists: false, size: null, url }
    }
  })
}

export async function fetchText(path: string, opts?: { cacheBust?: boolean }): Promise<FetchResult<string>> {
  const config = await loadRuntimeConfig()
  const url = getDataUrl(path, config)
  const bust = opts?.cacheBust !== false

  let response: Response

  if (isS3Mode(config)) {
    response = await fetchViaS3(url)
  } else if (isLocalMode(config)) {
    response = await fetch(bust ? cacheBustUrl(url) : url, { credentials: 'include' })
  } else {
    response = await fetch(bust ? cacheBustUrl(url) : url)
  }

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

/**
 * Fetch a byte range of a text file (using Range header).
 * Falls back to a full fetch + truncation if the server doesn't support Range.
 */
export async function fetchPartialText(path: string, bytes: number, offset = 0): Promise<FetchResult<string>> {
  const config = await loadRuntimeConfig()
  const url = getDataUrl(path, config)

  let response: Response

  const headers: HeadersInit = { Range: `bytes=${offset}-${offset + bytes - 1}` }

  if (isS3Mode(config)) {
    response = await fetchViaS3(url)
  } else if (isLocalMode(config)) {
    response = await fetch(url, { credentials: 'include', headers })
  } else {
    response = await fetch(url, { headers })
  }

  if (!response.ok && response.status !== 206) {
    return { data: null, status: response.status }
  }

  const data = await response.text()
  return { data: data.slice(0, bytes), status: response.status }
}

export interface LineSummary {
  size: number
  /** First N bytes of the line (UTF-8 decoded), for extracting metadata like method names. */
  head: string
}

/**
 * Stream a text file and return per-line byte sizes and the first
 * `headBytes` characters of each line. The file content is never
 * held in memory — only the lightweight summaries are kept.
 */
const LINE_HEAD_BYTES = 256

export async function fetchLineSummaries(path: string): Promise<FetchResult<LineSummary[]>> {
  const config = await loadRuntimeConfig()
  const url = getDataUrl(path, config)

  let response: Response

  if (isS3Mode(config)) {
    response = await fetchViaS3(url)
  } else if (isLocalMode(config)) {
    response = await fetch(url, { credentials: 'include' })
  } else {
    response = await fetch(url)
  }

  if (!response.ok || !response.body) {
    return { data: null, status: response.status }
  }

  const lines: LineSummary[] = []
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  const newline = 10 // '\n'
  let lineSize = 0
  const headBuf: number[] = []

  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    for (let i = 0; i < value.length; i++) {
      if (value[i] === newline) {
        lines.push({ size: lineSize, head: decoder.decode(new Uint8Array(headBuf)) })
        lineSize = 0
        headBuf.length = 0
      } else {
        lineSize++
        if (headBuf.length < LINE_HEAD_BYTES) headBuf.push(value[i])
      }
    }
  }

  // Last line (if no trailing newline)
  if (lineSize > 0) {
    lines.push({ size: lineSize, head: decoder.decode(new Uint8Array(headBuf)) })
  }

  return { data: lines, status: response.status }
}

/**
 * Stream a text file and call `onLine` for each completed line.
 * Returns a promise that resolves when the stream is complete.
 */
export async function streamLineSummaries(
  path: string,
  onLine: (summary: LineSummary, index: number) => void,
): Promise<void> {
  const config = await loadRuntimeConfig()
  const url = getDataUrl(path, config)

  let response: Response

  if (isS3Mode(config)) {
    response = await fetchViaS3(url)
  } else if (isLocalMode(config)) {
    response = await fetch(url, { credentials: 'include' })
  } else {
    response = await fetch(url)
  }

  if (!response.ok || !response.body) return

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  const newline = 10
  let lineSize = 0
  const headBuf: number[] = []
  let lineIndex = 0

  for (;;) {
    const { done, value } = await reader.read()
    if (done) break
    for (let i = 0; i < value.length; i++) {
      if (value[i] === newline) {
        onLine({ size: lineSize, head: decoder.decode(new Uint8Array(headBuf)) }, lineIndex)
        lineSize = 0
        headBuf.length = 0
        lineIndex++
      } else {
        lineSize++
        if (headBuf.length < LINE_HEAD_BYTES) headBuf.push(value[i])
      }
    }
  }

  if (lineSize > 0) {
    onLine({ size: lineSize, head: decoder.decode(new Uint8Array(headBuf)) }, lineIndex)
  }
}
