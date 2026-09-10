import { describe, it, expect } from 'vitest'
import { COLORS, LEVELS, createColorScale, formatShortfall } from './runColorScale'

const BEST = COLORS[0]
const WORST = COLORS[LEVELS - 1]

// Evenly spaced values between lo and hi.
function spread(lo: number, hi: number, count: number): number[] {
  return Array.from({ length: count }, (_, i) => lo + (hi - lo) * (i / (count - 1)))
}

// Distinct colours a set takes, so a "flat" row is easy to assert.
function usedColors(values: number[], higherIsBetter = true): number[] {
  const scale = createColorScale(values, higherIsBetter)

  const palette: readonly string[] = COLORS

  return [...new Set(values.map((v) => palette.indexOf(scale.color(v))))].sort((a, b) => a - b)
}

describe('createColorScale', () => {
  it('paints identical runs one colour', () => {
    expect(usedColors(Array(20).fill(500))).toEqual([0])
  })

  it('keeps a set within 1% near the best colour', () => {
    // Ethrex in a real suite: 559.1 - 563.6 MGas/s.
    expect(usedColors(spread(559.1, 563.6, 24))).toEqual([0])
  })

  it('keeps a set within 3% in the green half', () => {
    // Reth in a real suite: 370.7 - 383.9 MGas/s.
    expect(Math.max(...usedColors(spread(370.7, 383.9, 26)))).toBeLessThanOrEqual(2)
  })

  it('never sends a run 8% off the best to the worst colour', () => {
    const values = spread(1100, 1200, 20)
    const scale = createColorScale(values, true)
    expect(scale.color(1100)).not.toBe(WORST)
    expect(scale.color(1200)).toBe(BEST)
  })

  it('spends the whole ramp on a set that genuinely spreads', () => {
    const values = [...spread(295, 312.6, 22), 228.9, 240, 255, 262, 270, 280]
    const used = usedColors(values)
    expect(used[0]).toBe(0)
    expect(used[used.length - 1]).toBe(LEVELS - 1)
  })

  it('marks a lone slow run without recolouring the stable ones', () => {
    const values = [...spread(1100, 1200, 29), 50]
    const scale = createColorScale(values, true)
    expect(scale.color(50)).toBe(WORST)
    // The 29 normal runs keep the colours they had without the outlier.
    expect(scale.color(1200)).toBe(BEST)
    expect(scale.color(1100)).not.toBe(WORST)
  })

  it('ignores a lone fast run when setting the reference', () => {
    // 29 runs at 1100 and one flukey 1400 must not paint the rest red.
    const scale = createColorScale([...Array(29).fill(1100), 1400], true)
    expect(scale.color(1100)).toBe(BEST)
  })

  it('grades a duration set the other way round', () => {
    const scale = createColorScale(spread(10, 30, 20), false)
    expect(scale.color(10)).toBe(BEST)
    expect(scale.color(30)).toBe(WORST)
  })

  it('ignores in-flight runs that report a zero duration', () => {
    // A live run is merged in with `steps: {}`, so its duration is 0. It
    // sorts to the fast end, where it used to drag the reference down:
    // three of them sent every finished run to the red half.
    const finished = Array.from({ length: 27 }, (_, i) => 100 + (i % 5))
    const clean = createColorScale(finished, false)
    const polluted = createColorScale([0, 0, 0, ...finished], false)
    expect(polluted.top).toBe(clean.top)
    expect(finished.map((v) => polluted.color(v))).toEqual(finished.map((v) => clean.color(v)))
  })

  it('survives a set of nothing but in-flight runs', () => {
    const scale = createColorScale([0, 0, 0, 0], false)
    expect(scale.hasData).toBe(false)
  })

  it('falls back to the neutral colour with no data', () => {
    const scale = createColorScale([], true)
    expect(scale.hasData).toBe(false)
    expect(scale.color(1)).toBe(COLORS[Math.floor(LEVELS / 2)])
  })
})

describe('formatShortfall', () => {
  it('keeps one decimal below 10% and drops it above', () => {
    expect(formatShortfall(0.015)).toBe('1.5%')
    expect(formatShortfall(0.0796)).toBe('8.0%')
    expect(formatShortfall(0.24)).toBe('24%')
  })
})
