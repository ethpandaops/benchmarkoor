import { parseEESTName, type EESTNameParts } from './eestName'

// Aliases let users type the more natural word and we normalize internally.
const FIELD_ALIASES: Record<string, string> = {
  fn: 'fn',
  function: 'fn',
  file: 'file',
  path: 'path',
  fork: 'fork',
  opcode: 'opcode',
  gas: 'benchmark',
  benchmark: 'benchmark',
  label: 'label',
}

/**
 * Returns true when `name` matches the search `query`.
 *
 * - Free-text terms: case-insensitive substring matches against the raw
 *   test name (decomposed parts are subsets of the raw name).
 * - `key:value` terms: filter on a specific decomposed field. Recognized
 *   keys: `opcode`, `fork`, `gas`/`benchmark`, `file`, `fn`/`function`,
 *   `path`, `label`. Any other `key:value` is matched against the parsed
 *   params array (e.g. `mem_size:1024`, `code_size:0`).
 *
 * Multiple terms are combined with AND. Whitespace separates terms.
 */
export function testNameMatches(name: string, query: string): boolean {
  const trimmed = query.trim()
  if (!trimmed) return true

  const lowerName = name.toLowerCase()
  let parsed: EESTNameParts | null = null
  const ensureParsed = (): EESTNameParts => parsed ?? (parsed = parseEESTName(name))

  for (const term of trimmed.split(/\s+/)) {
    const colon = term.indexOf(':')
    if (colon > 0 && colon < term.length - 1) {
      const rawKey = term.slice(0, colon).toLowerCase()
      const value = term.slice(colon + 1).toLowerCase()
      if (!matchesField(ensureParsed(), rawKey, value)) return false
    } else {
      if (!lowerName.includes(term.toLowerCase())) return false
    }
  }
  return true
}

function matchesField(parsed: EESTNameParts, rawKey: string, value: string): boolean {
  if (!value) return true
  const key = FIELD_ALIASES[rawKey] ?? rawKey
  switch (key) {
    case 'fn':
      return parsed.fn?.toLowerCase().includes(value) ?? false
    case 'file':
      return parsed.file?.toLowerCase().includes(value) ?? false
    case 'path':
      return parsed.path?.toLowerCase().includes(value) ?? false
    case 'fork':
      return parsed.fork?.toLowerCase().includes(value) ?? false
    case 'opcode':
      return parsed.opcode?.toLowerCase().includes(value) ?? false
    case 'benchmark':
      return parsed.benchmark?.toLowerCase().includes(value) ?? false
    case 'label':
      return parsed.labels.some((l) => l.toLowerCase().includes(value))
    default:
      // Treat unknown keys as a param-key filter (`mem_size:1024`).
      // Exact key match, substring on value.
      return parsed.params.some(
        ({ key: k, value: v }) => k.toLowerCase() === rawKey && v.toLowerCase().includes(value),
      )
  }
}
