/**
 * Placing a floating tooltip so it stays inside the window.
 *
 * A tooltip anchored to a small target and centred on it runs off the edge as
 * soon as the target sits near one: half the tooltip ends up outside the
 * window, where it cannot be read. The maths is the same wherever this
 * happens, so it lives here where it can be tested without a DOM.
 */

/** The gap kept between the tooltip and both the anchor and the window edge. */
export const TOOLTIP_MARGIN = 8

/** The part of a DOMRect the placement needs. */
export interface AnchorRect {
  left: number
  top: number
  width: number
  bottom: number
}

export interface Size {
  width: number
  height: number
}

export interface Viewport {
  width: number
  height: number
}

export interface TooltipPosition {
  left: number
  top: number
}

/**
 * Returns the viewport coordinates for a tooltip of `size` anchored to
 * `anchor`. Horizontally it is centred on the anchor, then pulled back inside
 * the window. Vertically it sits above the anchor, and flips below when the
 * top of the window is too close.
 *
 * Both axes are a last-resort clamp as well as a preference, so a tooltip
 * larger than the window is pinned to the top-left corner rather than being
 * centred half outside it.
 */
export function positionTooltip(
  anchor: AnchorRect,
  size: Size,
  viewport: Viewport,
  margin: number = TOOLTIP_MARGIN,
): TooltipPosition {
  const centred = anchor.left + anchor.width / 2 - size.width / 2
  const maxLeft = viewport.width - size.width - margin

  // A tooltip wider than the window cannot honour both edges. Pin it to the
  // left one, so the text starts where it can be read.
  const left = maxLeft < margin ? margin : Math.min(Math.max(centred, margin), maxLeft)

  // Above the anchor by preference, below it when there is no room above.
  let top = anchor.top - margin - size.height
  if (top < margin) top = anchor.bottom + margin

  // Flipping below can overshoot the bottom on a short window, so clamp.
  const maxTop = viewport.height - size.height - margin
  if (top > maxTop) top = Math.max(margin, maxTop)

  return { left, top }
}
