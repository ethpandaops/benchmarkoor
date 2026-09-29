import { describe, expect, it } from 'vitest'

import { formatApproxBandwidth, formatApproxCount, formatBytes, formatDiskSize, withinTolerance } from './format'

describe('formatBytes', () => {
  it('formats byte counts by magnitude', () => {
    expect(formatBytes(2048)).toBe('2.0 KB')
  })

  it('renders a placeholder for a value the client never emitted', () => {
    expect(formatBytes(undefined)).toBe('-')
    expect(formatBytes(NaN)).toBe('-')
  })
})

describe('formatApproxCount', () => {
  it('rounds to 2 significant figures', () => {
    expect(formatApproxCount(667803)).toBe('~670K')
    expect(formatApproxCount(671020)).toBe('~670K')
    expect(formatApproxCount(2084)).toBe('~2.1K')
    expect(formatApproxCount(1234567)).toBe('~1.2M')
    expect(formatApproxCount(950)).toBe('~950')
  })
})

describe('formatApproxBandwidth', () => {
  it('rounds to 2 significant figures in a binary unit', () => {
    expect(formatApproxBandwidth(206.4 * 1024 ** 3)).toBe('~210 GB/s')
    expect(formatApproxBandwidth(99.3 * 1024 ** 3)).toBe('~99 GB/s')
    expect(formatApproxBandwidth(500 * 1024 ** 2)).toBe('~500 MB/s')
    expect(formatApproxBandwidth(1.94 * 1024 ** 3)).toBe('~1.9 GB/s')
  })
})

describe('formatDiskSize', () => {
  it('uses decimal units', () => {
    expect(formatDiskSize(4_000_787_030_016)).toBe('4.00 TB')
    expect(formatDiskSize(512_110_190_592)).toBe('512.1 GB')
  })
})

describe('withinTolerance', () => {
  it('matches values within 15% of each other', () => {
    expect(withinTolerance([700480, 667803])).toBe(true)
    expect(withinTolerance([110.6, 99.3])).toBe(true)
    expect(withinTolerance([700480, 1012345])).toBe(false)
  })

  it('treats a missing value as a difference', () => {
    expect(withinTolerance([100, undefined])).toBe(false)
    expect(withinTolerance([undefined, undefined])).toBe(true)
  })

  it('handles zero', () => {
    expect(withinTolerance([0, 0])).toBe(true)
    expect(withinTolerance([0, 5])).toBe(false)
  })
})
