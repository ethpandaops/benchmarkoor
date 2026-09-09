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

// Text classes for the same five steps. The tile colours are fills and
// are too light for small text, so a value rendered as text uses these.
export const THRESHOLD_TEXT_CLASSES = [
  'text-green-600 dark:text-green-400',
  'text-lime-600 dark:text-lime-400',
  'text-yellow-600 dark:text-yellow-400',
  'text-orange-600 dark:text-orange-400',
  'text-red-600 dark:text-red-400',
] as const

/**
 * Step of the scale a "higher is better" ratio falls in:
 *   ratio >= 2     → 0  very fast
 *   ratio >= 1.5   → 1  fast
 *   ratio >= 1     → 2  at the limit
 *   ratio >= 0.5   → 3  slow
 *   ratio <  0.5   → 4  very slow
 */
function thresholdStep(ratio: number): number {
  if (ratio >= 2) return 0
  if (ratio >= 1.5) return 1
  if (ratio >= 1) return 2
  if (ratio >= 0.5) return 3
  return 4
}

// A duration is better when it is shorter, so the ratio inverts.
function durationRatio(nanoseconds: number, slowMs: number): number {
  if (nanoseconds <= 0) return Infinity
  return (slowMs * 1_000_000) / nanoseconds
}

/** Map a MGas/s value to a colour, scaled relative to the threshold. */
export function getColorByThreshold(value: number, threshold: number): string {
  return THRESHOLD_COLORS[thresholdStep(value / threshold)]
}

/** Map a MGas/s value to a text class, scaled relative to the threshold. */
export function getTextClassByThreshold(value: number, threshold: number): string {
  return THRESHOLD_TEXT_CLASSES[thresholdStep(value / threshold)]
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
 * Map a payload duration to a colour, scaled against the slow limit:
 *   duration <= limit/2   → green   (very fast)
 *   duration <= limit/1.5 → lime    (fast)
 *   duration <= limit     → yellow  (at the limit)
 *   duration <= limit*2   → orange  (slow)
 *   duration >  limit*2   → red     (very slow)
 */
export function getColorByDuration(nanoseconds: number, slowMs: number): string {
  return THRESHOLD_COLORS[thresholdStep(durationRatio(nanoseconds, slowMs))]
}

/** Map a payload duration to a text class, scaled against the slow limit. */
export function getTextClassByDuration(nanoseconds: number, slowMs: number): string {
  return THRESHOLD_TEXT_CLASSES[thresholdStep(durationRatio(nanoseconds, slowMs))]
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
