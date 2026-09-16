/**
 * `overhead_baseline_True` test variants are calibration helpers: they run the
 * test's keccak-overhead loop without the real workload (~7 Mgas against a
 * 200M+ target), existing only so the workload cost can be isolated by
 * subtraction. Any aggregate that includes them — average MGas/s, heatmaps,
 * comparisons — is skewed low, so the UI treats them as if they don't exist.
 * They still execute and remain in result.json / the index for offline
 * analysis.
 */
const BASELINE_RE = /overhead_baseline_true/i

export function isBaselineTest(name: string): boolean {
  return BASELINE_RE.test(name)
}

/**
 * Returns `record` without baseline-test keys. Works on any test-name-keyed
 * map (RunResult.tests, SuiteStats). Returns the input unchanged when no key
 * matches, so unaffected suites don't pay a re-allocation.
 */
export function withoutBaselineTests<T>(record: Record<string, T>): Record<string, T> {
  let hasBaseline = false
  for (const name in record) {
    if (isBaselineTest(name)) {
      hasBaseline = true
      break
    }
  }
  if (!hasBaseline) return record

  const filtered: Record<string, T> = {}
  for (const [name, value] of Object.entries(record)) {
    if (!isBaselineTest(name)) filtered[name] = value
  }
  return filtered
}
