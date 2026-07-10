import { sha256 } from '@noble/hashes/sha2.js'
import { bytesToHex } from '@noble/hashes/utils.js'

// Mirror of the Go backend's sanitizeResultPath (pkg/executor/results.go).
//
// At write time, benchmarkoor truncates+hashes any result-path component longer
// than maxResultPathComponent so per-test directory names stay under the
// filesystem / S3 key length limit. The stored key is therefore NOT the raw
// test name for long tests. The UI builds per-test artifact URLs
// (`runs/{runId}/{testName}/{step}.response`, etc.) from the full test name, so
// without applying the SAME transform those long-named tests 404. Keep this
// constant and the algorithm in sync with the Go side.
const MAX_RESULT_PATH_COMPONENT = 200

function sanitizeComponent(component: string): string {
  // Go measures length in bytes and slices the string by bytes; test names are
  // ASCII in practice, but encode/decode to stay byte-faithful.
  const bytes = new TextEncoder().encode(component)
  if (bytes.length <= MAX_RESULT_PATH_COMPONENT) return component

  // first (200 - 17) bytes of the name, then "-" + first 8 bytes of its sha256
  // as hex (16 chars) => exactly 200 chars, byte-identical to the Go output:
  //   p[:maxResultPathComponent-17] + "-" + hex(sha256(p)[:8])
  const head = new TextDecoder().decode(bytes.slice(0, MAX_RESULT_PATH_COMPONENT - 17))
  const digest = bytesToHex(sha256(bytes)).slice(0, 16)
  return `${head}-${digest}`
}

/**
 * Sanitize a data path so it matches the key the backend actually wrote.
 * Applied per '/'-separated component (mirrors Go's sanitizeResultPath), so
 * short components — runIds, suite hashes, step filenames — pass through
 * unchanged and only over-long test-name directories are truncated+hashed.
 */
export function sanitizeResultPath(path: string): string {
  if (path.indexOf('/') === -1) return sanitizeComponent(path)
  return path.split('/').map(sanitizeComponent).join('/')
}
