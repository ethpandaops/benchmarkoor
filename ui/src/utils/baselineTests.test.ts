import { describe, expect, it } from 'vitest'
import { isBaselineTest, withoutBaselineTests } from './baselineTests'

const BASELINE_NAME =
  'tests/benchmark/stateful/bloatnet/test_sload.py::test_sload_bloated[fork_Amsterdam-blockchain_test_stateful_engine-overhead_baseline_True-benchmark-gas-value_200M]'
const WORKLOAD_NAME =
  'tests/benchmark/stateful/bloatnet/test_sload.py::test_sload_bloated[fork_Amsterdam-blockchain_test_stateful_engine-overhead_baseline_False-benchmark-gas-value_200M]'

describe('isBaselineTest', () => {
  it('matches overhead_baseline_True variants', () => {
    expect(isBaselineTest(BASELINE_NAME)).toBe(true)
  })

  it('is case-insensitive', () => {
    expect(isBaselineTest(BASELINE_NAME.replace('_True', '_true'))).toBe(true)
  })

  it('does not match the _False workload twin', () => {
    expect(isBaselineTest(WORKLOAD_NAME)).toBe(false)
  })

  it('does not match unrelated tests', () => {
    expect(isBaselineTest('tests/benchmark/compute/instruction/test_keccak.py::test_keccak')).toBe(
      false,
    )
  })
})

describe('withoutBaselineTests', () => {
  it('drops baseline keys and keeps the rest', () => {
    const filtered = withoutBaselineTests({ [BASELINE_NAME]: 1, [WORKLOAD_NAME]: 2 })
    expect(Object.keys(filtered)).toEqual([WORKLOAD_NAME])
  })

  it('returns the same reference when nothing matches', () => {
    const input = { [WORKLOAD_NAME]: 1 }
    expect(withoutBaselineTests(input)).toBe(input)
  })
})
