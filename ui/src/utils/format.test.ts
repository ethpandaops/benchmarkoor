import { describe, expect, it } from 'vitest'

import { formatBytes } from './format'

describe('formatBytes', () => {
  it('formats byte counts by magnitude', () => {
    expect(formatBytes(2048)).toBe('2.0 KB')
  })

  it('renders a placeholder for a value the client never emitted', () => {
    expect(formatBytes(undefined)).toBe('-')
    expect(formatBytes(NaN)).toBe('-')
  })
})
