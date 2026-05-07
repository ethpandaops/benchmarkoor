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
 * Hint text for filter input tooltips. Kept here so every search box stays
 * in sync as we tweak supported syntax.
 */
export const TEST_FILTER_HINT =
  'Free text matches the raw name. Or filter by extracted fields:\n' +
  'opcode:ORIGIN  gas:90M  fork:Amsterdam  file:tx_context  fn:codecopy  path:compute  label:LOG1\n' +
  'Unrecognized keys hit params: mem_size:1024  code_size:0\n' +
  'Use `=` for exact match instead of substring (chip clicks use this): opcode=ORIGIN  code_size=0\n' +
  'Multiple terms are AND.'

type StructuredTerm = { key: string; value: string; exact: boolean }

/**
 * Parse a structured term. `key:value` is substring match (default for typed
 * queries). `key=value` is an exact match (used by chip clicks). Returns null
 * for free-text or malformed terms.
 */
function parseStructuredTerm(term: string): StructuredTerm | null {
  const colonIdx = term.indexOf(':')
  const eqIdx = term.indexOf('=')

  let sepIdx = -1
  let exact = false
  if (eqIdx > 0 && (colonIdx < 0 || eqIdx < colonIdx)) {
    sepIdx = eqIdx
    exact = true
  } else if (colonIdx > 0) {
    sepIdx = colonIdx
    exact = false
  } else {
    return null
  }
  if (sepIdx === term.length - 1) return null

  return {
    key: term.slice(0, sepIdx).toLowerCase(),
    value: term.slice(sepIdx + 1).toLowerCase(),
    exact,
  }
}

/**
 * Returns true when `name` matches the search `query`.
 *
 * - Free-text terms: case-insensitive substring matches against the raw
 *   test name (decomposed parts are subsets of the raw name).
 * - `key:value` terms: substring match on a specific decomposed field
 *   (e.g. `opcode:ORIG` matches ORIGIN).
 * - `key=value` terms: exact match on the field (e.g. `calldata_size=0`
 *   only matches calldata_size_0, not calldata_size_1024). Chip clicks
 *   produce this form.
 *
 * Recognized keys: `opcode`, `fork`, `gas`/`benchmark`, `file`,
 * `fn`/`function`, `path`, `label`. Any other key hits the parsed params.
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
    const structured = parseStructuredTerm(term)
    if (structured) {
      if (!matchesField(ensureParsed(), structured)) return false
    } else {
      if (!lowerName.includes(term.toLowerCase())) return false
    }
  }
  return true
}

/** Split a query string into whitespace-separated terms. */
export function splitQuery(query: string): string[] {
  return query.trim().split(/\s+/).filter(Boolean)
}

/**
 * Returns the canonical dimension key targeted by a single term, or null if
 * the term is free-text. Used to drop dimension-targeted terms when
 * computing contextual facet counts (so a filter on opcode doesn't shrink
 * the OPCODE facet to a single chip).
 */
export function queryTermDimension(term: string): string | null {
  const structured = parseStructuredTerm(term)
  if (!structured) return null
  return FIELD_ALIASES[structured.key] ?? structured.key
}

/** Remove every term targeting `canonicalDim` from the query. */
export function queryWithoutDimension(query: string, canonicalDim: string): string {
  return splitQuery(query)
    .filter((t) => queryTermDimension(t) !== canonicalDim)
    .join(' ')
}

/** Returns true if the query already contains the given term (case-insensitive). */
export function searchQueryContains(query: string, term: string): boolean {
  const target = term.toLowerCase()
  return splitQuery(query).some((t) => t.toLowerCase() === target)
}

/**
 * Toggle a term in the query. If present (case-insensitive), it's removed;
 * otherwise it's appended. Whitespace is normalized to single spaces.
 */
export function toggleSearchTerm(query: string, term: string): string {
  const target = term.toLowerCase()
  const tokens = splitQuery(query)
  const idx = tokens.findIndex((t) => t.toLowerCase() === target)
  if (idx >= 0) tokens.splice(idx, 1)
  else tokens.push(term)
  return tokens.join(' ')
}

function matchesField(parsed: EESTNameParts, term: StructuredTerm): boolean {
  if (!term.value) return true
  const key = FIELD_ALIASES[term.key] ?? term.key
  const cmp = (haystack: string | undefined): boolean => {
    if (haystack === undefined) return false
    const h = haystack.toLowerCase()
    return term.exact ? h === term.value : h.includes(term.value)
  }
  switch (key) {
    case 'fn':
      return cmp(parsed.fn)
    case 'file':
      return cmp(parsed.file)
    case 'path':
      return cmp(parsed.path)
    case 'fork':
      return cmp(parsed.fork)
    case 'opcode':
      return cmp(parsed.opcode)
    case 'benchmark':
      return cmp(parsed.benchmark)
    case 'label':
      return parsed.labels.some((l) => cmp(l))
    default:
      // Param-key filter (`mem_size:1024` or `mem_size=1024`). Exact key
      // match, substring or exact on value depending on the separator.
      return parsed.params.some(
        ({ key: k, value: v }) => k.toLowerCase() === term.key && cmp(v),
      )
  }
}
