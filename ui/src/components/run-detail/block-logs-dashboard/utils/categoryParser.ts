import type { TestCategory } from '../types'

/**
 * Parse category from test name by looking at test file path and function name.
 * Categories are based on the benchmark test structure:
 * - benchmark/compute/instruction/* - EVM instruction tests
 * - benchmark/compute/precompile/* - Precompile tests
 * - benchmark/compute/scenario/* - Scenario tests
 */
export function parseCategory(testName: string): TestCategory {
  const lowerName = testName.toLowerCase()

  // Scenario tests
  if (lowerName.includes('/scenario/')) {
    return 'scenario'
  }

  // Precompile tests
  if (lowerName.includes('/precompile/')) {
    if (lowerName.includes('test_alt_bn128') || lowerName.includes('bn128')) {
      return 'bn128'
    }
    if (lowerName.includes('test_bls12_381') || lowerName.includes('bls12')) {
      return 'bls'
    }
    return 'precompile'
  }

  // Instruction tests - match by test file name
  if (lowerName.includes('/instruction/')) {
    if (lowerName.includes('test_arithmetic') || lowerName.includes('test_bitwise') || lowerName.includes('test_comparison')) {
      return 'arithmetic'
    }
    if (lowerName.includes('test_memory')) {
      return 'memory'
    }
    if (lowerName.includes('test_storage')) {
      return 'storage'
    }
    if (lowerName.includes('test_stack')) {
      return 'stack'
    }
    if (lowerName.includes('test_control_flow')) {
      return 'control'
    }
    if (lowerName.includes('test_keccak')) {
      return 'keccak'
    }
    if (lowerName.includes('test_log')) {
      return 'log'
    }
    if (lowerName.includes('test_account_query')) {
      return 'account'
    }
    if (lowerName.includes('test_call_context')) {
      return 'call'
    }
    if (lowerName.includes('test_block_context') || lowerName.includes('test_tx_context')) {
      return 'context'
    }
    if (lowerName.includes('test_system')) {
      return 'system'
    }
  }

  return 'other'
}
