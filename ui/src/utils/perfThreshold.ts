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
