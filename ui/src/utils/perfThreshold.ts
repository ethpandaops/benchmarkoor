// Shared "slow threshold" model. The user picks a single MGas/s threshold
// for the run-detail page; the Performance Heatmap, the distribution
// histogram, and the Dimension Breakdown bars all colour against it.

export const MIN_THRESHOLD = 1
export const MAX_THRESHOLD = 1000
export const DEFAULT_THRESHOLD = 60

export const THRESHOLD_COLORS = [
  '#22c55e', // very fast — green
  '#84cc16', // fast — lime
  '#eab308', // at threshold — yellow
  '#f97316', // slow — orange
  '#ef4444', // very slow — red
] as const

/**
 * Map a MGas/s value to a colour, scaled relative to the threshold:
 *   ratio >= 2     → green   (very fast)
 *   ratio >= 1.5   → lime    (fast)
 *   ratio >= 1     → yellow  (at threshold)
 *   ratio >= 0.5   → orange  (slow)
 *   ratio <  0.5   → red     (very slow)
 */
export function getColorByThreshold(value: number, threshold: number): string {
  const ratio = value / threshold
  if (ratio >= 2) return THRESHOLD_COLORS[0]
  if (ratio >= 1.5) return THRESHOLD_COLORS[1]
  if (ratio >= 1) return THRESHOLD_COLORS[2]
  if (ratio >= 0.5) return THRESHOLD_COLORS[3]
  return THRESHOLD_COLORS[4]
}

// Slow-payload model. The user picks a duration limit on the run-detail
// page; the Performance Heatmap marks every test with an
// engine_newPayload call above that limit.

export const MIN_SLOW_MS = 100
export const MAX_SLOW_MS = 20_000
export const SLOW_STEP_MS = 100
export const DEFAULT_SLOW_MS = 3_000

// Marker colour for a slow payload. Distinct from the MGas/s scale
// (green → red) and from the red failure ring.
export const SLOW_COLOR = '#d946ef' // fuchsia-500

/**
 * Map a payload duration to a colour, scaled against the slow limit.
 * Short is good, so the ratio is inverted before it goes through the
 * same five steps as the MGas/s scale:
 *   duration <= limit/2   → green   (very fast)
 *   duration <= limit/1.5 → lime    (fast)
 *   duration <= limit     → yellow  (at the limit)
 *   duration <= limit*2   → orange  (slow)
 *   duration >  limit*2   → red     (very slow)
 */
export function getColorByDuration(nanoseconds: number, slowMs: number): string {
  if (nanoseconds <= 0) return THRESHOLD_COLORS[0]
  return getColorByThreshold((slowMs * 1_000_000) / nanoseconds, 1)
}

/** Report if a payload duration in nanoseconds is above the limit. */
export function isSlowPayload(nanoseconds: number, slowMs: number): boolean {
  return nanoseconds > slowMs * 1_000_000
}

/** Render a slow limit in milliseconds as seconds, e.g. 3000 -> "3s". */
export function formatSlowMs(slowMs: number): string {
  const seconds = slowMs / 1000
  return `${Number.isInteger(seconds) ? seconds : seconds.toFixed(1)}s`
}
