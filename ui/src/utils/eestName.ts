// Parser for Ethereum Execution Specification Tests (EEST) test names.
//
// Format observed across the test corpus:
//   test_<file>.py__test_<fn>[<param>-<param>-...].txt
//
// Each <param> is dash-joined and is usually `<key>_<value>` (with the key
// possibly containing underscores — we split on the LAST `_`). A few params
// are free-form descriptive labels with spaces (e.g. `0 bytes without value`)
// or no underscore at all; these go into `labels`.
//
// We lift well-known keys (fork / opcode / benchmark gas amount) into named
// fields and drop the always-present `benchmark_test` marker since it carries
// no information. Anything else stays as a generic `{key, value}` chip.

export interface EESTNameParts {
  /** Original input, unchanged — always available as source of truth. */
  raw: string
  /** True if the input matched the EEST file/function format. */
  isEEST: boolean
  /** File slug (without `test_` prefix and `.py` suffix). e.g. `tx_context` */
  file?: string
  /** Function slug (without `test_` prefix). e.g. `call_frame_context_ops` */
  fn?: string
  /** Fork name from `fork_<X>`. e.g. `Amsterdam` */
  fork?: string
  /** Opcode under test from `opcode_<X>`. e.g. `ORIGIN`, `LOG1` */
  opcode?: string
  /** Benchmark gas amount from `benchmark_<N>M`. e.g. `90M`, `300M` */
  benchmark?: string
  /** Other key/value params not captured above. */
  params: { key: string; value: string }[]
  /** Tokens that aren't `key_value`-shaped (free-form labels). */
  labels: string[]
  /** Compact one-line summary for use in tooltips and tight cells. */
  short: string
}

const FILE_FN_RE = /^test_(.+?)\.py__test_(.+?)(?:\[(.*)\])?(?:\.txt)?$/
const BENCHMARK_GAS_RE = /^\d+[KMG]?$/

export function parseEESTName(raw: string): EESTNameParts {
  const m = FILE_FN_RE.exec(raw)
  if (!m) {
    return { raw, isEEST: false, params: [], labels: [], short: raw }
  }

  const [, file, fn, paramBlock = ''] = m
  const tokens = paramBlock ? paramBlock.split('-') : []

  let fork: string | undefined
  let opcode: string | undefined
  let benchmark: string | undefined
  const params: { key: string; value: string }[] = []
  const labels: string[] = []

  for (const token of tokens) {
    if (!token) continue
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
        // `benchmark_test` is the always-present EEST marker — drop it.
        params.push({ key, value })
      }
    } else {
      params.push({ key, value })
    }
  }

  const shortParts: string[] = []
  if (fn) shortParts.push(fn)
  if (opcode) shortParts.push(opcode)
  if (benchmark) shortParts.push(benchmark)
  const short = shortParts.length > 0 ? shortParts.join(' · ') : raw

  return { raw, isEEST: true, file, fn, fork, opcode, benchmark, params, labels, short }
}

/**
 * Pure formatter usable from non-React contexts (e.g. canvas drawing).
 * Returns a short single-line string suitable for tooltips and tight cells.
 */
export function formatTestName(name: string, mode: 'raw' | 'decomposed'): string {
  if (mode === 'raw') return name
  return parseEESTName(name).short
}
