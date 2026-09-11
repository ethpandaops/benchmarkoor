import { describe, expect, it } from 'vitest'

import { compareOptional } from './useProcessedData'

describe('compareOptional', () => {
  it('orders reported values by magnitude', () => {
    expect(compareOptional(10, 20)).toBeLessThan(0)
    expect(compareOptional(20, 10)).toBeGreaterThan(0)
    expect(compareOptional(10, 10)).toBe(0)
  })

  it('groups unreported values instead of ranking them as zero', () => {
    expect(compareOptional(undefined, undefined)).toBe(0)
    expect(compareOptional(undefined, 0)).toBeGreaterThan(0)
    expect(compareOptional(0, undefined)).toBeLessThan(0)
  })
})
