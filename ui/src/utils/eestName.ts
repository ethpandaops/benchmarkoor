// Parser for Ethereum Execution Specification Tests (EEST) test names.
//
// Two formats observed across the corpus:
//
//   1. `[<run_id>/]test_<file>.py__test_<fn>[<params>].txt`
//   2. `<path>/test_<file>.py::test_<fn>[<params>]`
//
// Plus a rare variant where the gas amount sits *outside* the closing
// bracket: `...test_fn[<params>]_<N>M.txt`.
//
// Each <param> is dash-joined and usually `<key>_<value>` (the key may
// contain underscores, so we split on the LAST `_`). Some are free-form
// descriptive labels with spaces (`0 bytes without value`) or no
// underscore at all; those go into `labels`.
//
// We lift well-known keys (fork / opcode / benchmark gas) into named
// fields, drop noisy always-present markers (`benchmark_test`,
// `blockchain_test_engine_x`, standalone `benchmark`/`gas`), and recognize
// the gas amount in three places: `benchmark_<N>M`, `value_<N>M` (the new
// `benchmark-gas-value_<N>M` triple), and the trailing `]_<N>M.txt` form.

export interface EESTNameParts {
  /** Original input, unchanged — always available as source of truth. */
  raw: string
  /** True if the input matched the EEST file/function format. */
  isEEST: boolean
  /**
   * Optional directory prefix (e.g. `benchmark/compute/instruction`).
   * Run-id-style numeric prefixes like `000001` are hidden as noise.
   */
  path?: string
  /** File slug (without `test_` prefix and `.py` suffix). e.g. `tx_context` */
  file?: string
  /** Function slug (without `test_` prefix). e.g. `call_frame_context_ops` */
  fn?: string
  /** Fork name from `fork_<X>`. e.g. `Amsterdam` */
  fork?: string
  /** Opcode under test from `opcode_<X>`. e.g. `ORIGIN`, `LOG1` */
  opcode?: string
  /** Benchmark gas amount. e.g. `90M`, `300M` */
  benchmark?: string
  /** Other key/value params not captured above. */
  params: { key: string; value: string }[]
  /** Tokens that aren't `key_value`-shaped (free-form labels). */
  labels: string[]
  /** Compact one-line summary for use in tooltips and tight cells. */
  short: string
}

// Match either `__` (old) or `::` (new) between file and function, with an
// optional path prefix and an optional `]_<N>M` trailer before `.txt`.
const FILE_FN_RE = /^(?:(.+?)\/)?test_(.+?)\.py(?:__|::)test_(.+?)(?:\[(.*?)\])?(?:_(\d+[KMG]))?(?:\.txt)?$/
const BENCHMARK_GAS_RE = /^\d+[KMG]?$/
const VALUE_GAS_RE = /^\d+[KMG]$/

const IGNORE_TOKENS = new Set([
  'benchmark_test',
  'blockchain_test',
  'blockchain_test_engine_x',
  'benchmark',
  'gas',
])

export function parseEESTName(raw: string): EESTNameParts {
  const m = FILE_FN_RE.exec(raw)
  if (!m) {
    return { raw, isEEST: false, params: [], labels: [], short: raw }
  }

  const [, pathRaw, file, fn, paramBlock = '', trailingGas] = m

  // Hide run-id-style numeric path prefixes (e.g. `000001`) — they're not
  // useful to display and clutter the chip line.
  const path = pathRaw && !/^\d+$/.test(pathRaw) ? pathRaw : undefined

  let fork: string | undefined
  let opcode: string | undefined
  let benchmark: string | undefined
  const params: { key: string; value: string }[] = []
  const labels: string[] = []

  for (const token of (paramBlock ? paramBlock.split('-') : [])) {
    if (!token) continue
    if (IGNORE_TOKENS.has(token)) continue

    const lastUnderscore = token.lastIndexOf('_')
    // No underscore, key would be empty, key has whitespace, or value is empty
    // → treat the whole token as a free-form label.
    if (
      lastUnderscore <= 0 ||
      lastUnderscore === token.length - 1 ||
      /\s/.test(token.slice(0, lastUnderscore))
    ) {
      labels.push(token)
      continue
    }

    const key = token.slice(0, lastUnderscore)
    const value = token.slice(lastUnderscore + 1)

    if (key === 'fork') {
      fork = value
    } else if (key === 'opcode') {
      opcode = value
    } else if (key === 'benchmark') {
      if (BENCHMARK_GAS_RE.test(value)) {
        benchmark = value
      } else if (value !== 'test') {
        params.push({ key, value })
      }
    } else if (key === 'value' && VALUE_GAS_RE.test(value)) {
      // New EEST format: gas as the `value_<N>M` tail of the
      // `benchmark-gas-value_<N>M` triple.
      benchmark = value
    } else {
      params.push({ key, value })
    }
  }

  // Trailing `]_<N>M.txt` form, only used when no in-bracket gas was found.
  if (!benchmark && trailingGas) benchmark = trailingGas

  const shortParts: string[] = []
  if (fn) shortParts.push(fn)
  if (benchmark) shortParts.push(benchmark)
  if (opcode) shortParts.push(opcode)
  const short = shortParts.length > 0 ? shortParts.join(' · ') : raw

  return { raw, isEEST: true, path, file, fn, fork, opcode, benchmark, params, labels, short }
}

/**
 * Pure formatter usable from non-React contexts (e.g. canvas drawing).
 * Returns a short single-line string suitable for tooltips and tight cells.
 */
export function formatTestName(name: string, mode: 'raw' | 'decomposed'): string {
  if (mode === 'raw') return name
  return parseEESTName(name).short
}

/**
 * Same intent as `formatTestName`, but includes every decomposed field
 * (file, fn, gas, opcode, fork, params, labels) — for chart tooltips and
 * other contexts where we have room to show everything.
 */
export function formatTestNameLong(name: string, mode: 'raw' | 'decomposed'): string {
  if (mode === 'raw') return name
  const p = parseEESTName(name)
  if (!p.isEEST) return name
  const parts: string[] = []
  if (p.file) parts.push(p.file)
  if (p.fn) parts.push(p.fn)
  if (p.benchmark) parts.push(p.benchmark)
  if (p.opcode) parts.push(p.opcode)
  if (p.fork) parts.push(`fork:${p.fork}`)
  for (const { key, value } of p.params) parts.push(`${key}:${value}`)
  for (const label of p.labels) parts.push(label)
  return parts.length > 0 ? parts.join(' · ') : name
}
