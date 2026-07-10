import { describe, it, expect } from 'vitest'
import { sanitizeResultPath } from './resultPath'

const rep = (c: string, n: number) => c.repeat(n)

describe('sanitizeResultPath', () => {
  // Mirrors pkg/executor/sanitize_path_test.go — keep the cases (and the 200
  // cap) in sync with the Go implementation they must match byte-for-byte.

  it('leaves short components unchanged', () => {
    const short = 'benchmark/stateful/bloatnet/test_x.py::test_y[fork_Amsterdam]'
    expect(sanitizeResultPath(short)).toBe(short)
  })

  it('leaves a component of exactly 200 chars unchanged (boundary)', () => {
    const p = `runs/abc/${rep('a', 200)}/test.response`
    expect(sanitizeResultPath(p)).toBe(p)
  })

  it('truncates+hashes an over-long leaf to exactly 200 chars, preserving the prefix', () => {
    const out = sanitizeResultPath('benchmark/bloatnet/' + rep('a', 400))
    const parts = out.split('/')
    const leaf = parts[parts.length - 1]
    expect(leaf.length).toBe(200)
    expect(parts.slice(0, -1).join('/') + '/').toBe('benchmark/bloatnet/')
    // first 183 chars + "-" + 16 hex (first 8 bytes of sha256)
    expect(leaf).toMatch(/^a{183}-[0-9a-f]{16}$/)
  })

  it('maps distinct long names to distinct paths (hash uniqueness)', () => {
    const a = sanitizeResultPath('p/' + rep('a', 300) + 'X')
    const b = sanitizeResultPath('p/' + rep('a', 300) + 'Y')
    expect(a).not.toBe(b)
  })

  it('is deterministic', () => {
    const name = 'benchmark/bloatnet/' + rep('a', 400)
    expect(sanitizeResultPath(name)).toBe(sanitizeResultPath(name))
  })

  // Real EEST test names that triggered the 404: the 204-char one is stored
  // truncated+hashed, the 198-char one verbatim.
  const long204 =
    'test_single_opcode.py__test_account_access[fork_Amsterdam-benchmark_test-opcode_BALANCE-value_sent_0-account_mode_AccountMode.NON_EXISTING_ACCOUNT-cache_strategy_CacheStrategy.NO_CACHE-benchmark_200M].txt'
  const ok198 =
    'test_single_opcode.py__test_account_access[fork_Amsterdam-benchmark_test-opcode_CALL-value_sent_0-account_mode_AccountMode.EXISTING_CONTRACT-cache_strategy_CacheStrategy.NO_CACHE-benchmark_240M].txt'

  it('rewrites the 204-char test name to the stored key (golden value == Go output)', () => {
    expect(long204.length).toBe(204)
    const out = sanitizeResultPath(long204)
    expect(out.length).toBe(200)
    // Cross-checked against the Go backend's sanitizeResultPath.
    expect(out).toBe(long204.slice(0, 183) + '-ed95461550ccc1d7')
  })

  it('leaves the 198-char test name unchanged', () => {
    expect(ok198.length).toBe(198)
    expect(sanitizeResultPath(ok198)).toBe(ok198)
  })
})
