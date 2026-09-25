import { describe, it, expect } from 'vitest'

import { positionTooltip, TOOLTIP_MARGIN } from './tooltipPosition'

const VIEWPORT = { width: 1000, height: 800 }
const SIZE = { width: 300, height: 200 }

/** A 20px swatch, the size the run strip uses, at a given left offset. */
function swatch(left: number, top = 400) {
  return { left, top, width: 20, bottom: top + 20 }
}

describe('positionTooltip', () => {
  it('centres the tooltip on an anchor with room on both sides', () => {
    const { left } = positionTooltip(swatch(500), SIZE, VIEWPORT)

    // 500 + 10 - 150
    expect(left).toBe(360)
  })

  it('keeps the tooltip inside the left edge', () => {
    // The bug this was written for: the first swatch of the run strip sits
    // near the left edge, and a centred tooltip hangs half outside the window.
    const { left } = positionTooltip(swatch(24), SIZE, VIEWPORT)

    expect(left).toBe(TOOLTIP_MARGIN)
    expect(left).toBeGreaterThanOrEqual(0)
  })

  it('keeps the tooltip inside the right edge', () => {
    const { left } = positionTooltip(swatch(980), SIZE, VIEWPORT)

    expect(left).toBe(VIEWPORT.width - SIZE.width - TOOLTIP_MARGIN)
    expect(left + SIZE.width).toBeLessThanOrEqual(VIEWPORT.width)
  })

  it('pins a tooltip wider than the window to the left edge', () => {
    const { left } = positionTooltip(
      swatch(500),
      { width: 1200, height: 200 },
      VIEWPORT,
    )

    expect(left).toBe(TOOLTIP_MARGIN)
  })

  it('sits above the anchor when there is room', () => {
    const { top } = positionTooltip(swatch(500, 400), SIZE, VIEWPORT)

    // 400 - 8 - 200
    expect(top).toBe(192)
  })

  it('flips below the anchor when the top of the window is too close', () => {
    const anchor = swatch(500, 40)
    const { top } = positionTooltip(anchor, SIZE, VIEWPORT)

    expect(top).toBe(anchor.bottom + TOOLTIP_MARGIN)
  })

  it('leaves a flipped tooltip alone when it still fits', () => {
    // No room above, and below fits: the flip is the answer, unclamped.
    const shortViewport = { width: 1000, height: 260 }
    const anchor = swatch(500, 20)
    const { top } = positionTooltip(anchor, SIZE, shortViewport)

    expect(top).toBe(anchor.bottom + TOOLTIP_MARGIN)
    expect(top + SIZE.height).toBeLessThanOrEqual(shortViewport.height)
  })

  it('clamps to the bottom edge when flipping would overshoot', () => {
    // No room above, and below would now run past the bottom.
    const shortViewport = { width: 1000, height: 260 }
    const anchor = swatch(500, 30)
    const { top } = positionTooltip(anchor, SIZE, shortViewport)

    expect(top).toBeLessThan(anchor.bottom + TOOLTIP_MARGIN)
    expect(top).toBe(shortViewport.height - SIZE.height - TOOLTIP_MARGIN)
    expect(top).toBeGreaterThanOrEqual(0)
  })

  it('never returns a negative position, whatever the window', () => {
    const tiny = { width: 200, height: 150 }

    for (const left of [0, 100, 199]) {
      const pos = positionTooltip(swatch(left, 10), SIZE, tiny)

      expect(pos.left).toBeGreaterThanOrEqual(0)
      expect(pos.top).toBeGreaterThanOrEqual(0)
    }
  })
})
