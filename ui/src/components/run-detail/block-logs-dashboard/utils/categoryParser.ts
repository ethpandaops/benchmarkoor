import type { TestCategory } from '../types'

/**
 * Parse category from test name by looking for known patterns.
 * BN128 precompile tests typically contain _add_, _mul_, or _pairing_ in their names.
 */
export function parseCategory(testName: string): TestCategory {
  const lowerName = testName.toLowerCase()

  if (lowerName.includes('_add_') || lowerName.includes('_add-') || lowerName.includes('-add_') || lowerName.includes('-add-')) {
    return 'add'
  }

  if (lowerName.includes('_mul_') || lowerName.includes('_mul-') || lowerName.includes('-mul_') || lowerName.includes('-mul-') ||
      lowerName.includes('_multiply') || lowerName.includes('_scalar')) {
    return 'mul'
  }

  if (lowerName.includes('_pairing') || lowerName.includes('-pairing') || lowerName.includes('_pair_') || lowerName.includes('-pair-')) {
    return 'pairing'
  }

  // Additional patterns for BN128 tests
  if (lowerName.includes('ecadd') || lowerName.includes('bn256add')) {
    return 'add'
  }

  if (lowerName.includes('ecmul') || lowerName.includes('bn256scalarmul')) {
    return 'mul'
  }

  if (lowerName.includes('ecpairing') || lowerName.includes('bn256pairing')) {
    return 'pairing'
  }

  return 'other'
}
