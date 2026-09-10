// Shared "slow threshold" model. The user picks a single MGas/s threshold
// for the run-detail page; the Performance Heatmap, the distribution
// histogram, and the Dimension Breakdown bars all colour against it.

import { formatDuration } from './format'

export const MIN_THRESHOLD = 1
export const MAX_THRESHOLD = 1000
export const DEFAULT_THRESHOLD = 60

// Ten-step scale, green (well above the limit) to red (well below it).
// The same palette colours the percentile scales on the suite page, so
// every heatmap in the UI reads the same way.
export const THRESHOLD_COLORS = [
  '#047857', // deep green
  '#16a34a', // green
  '#32a71b', // green-lime
  '#84cc16', // lime
  '#eab308', // yellow — the limit itself
  '#f59e0b', // amber
  '#f97316', // orange
  '#f2542c', // deep orange
  '#ef4444', // red
  '#b91c1c', // deep red
] as const

// Lower bound of each step, as a multiple of the limit. The steps are
// symmetric around the limit: 1.25x pairs with 0.8x, 1.5x with 1/1.5x,
// 2x with 0.5x and 3x with 1/3x.
export const THRESHOLD_RATIOS = [3, 2, 1.5, 1.25, 1, 0.8, 1 / 1.5, 0.5, 1 / 3, 0] as const

// Step of a value that sits exactly on the limit — the yellow one.
export const THRESHOLD_LIMIT_STEP = 4

// Text classes for the same scale. The tile colours are fills and are
// too light for small text, so a value rendered as text uses these. Ten
// fills map onto five text colours, at the boundaries the earlier
// five-step scale used: >=2x green, >=1.5x lime, >=1x yellow, >=0.5x
// orange, below that red.
export const THRESHOLD_TEXT_CLASSES = [
  'text-green-600 dark:text-green-400',
  'text-green-600 dark:text-green-400',
  'text-lime-600 dark:text-lime-400',
  'text-yellow-600 dark:text-yellow-400',
  'text-yellow-600 dark:text-yellow-400',
  'text-orange-600 dark:text-orange-400',
  'text-orange-600 dark:text-orange-400',
  'text-orange-600 dark:text-orange-400',
  'text-red-600 dark:text-red-400',
  'text-red-600 dark:text-red-400',
] as const

/**
 * Step of the scale a "higher is better" ratio falls in. Step 0 is the
 * best (ratio >= 3) and step 9 the worst (ratio < 1/3).
 */
export function thresholdStep(ratio: number): number {
  for (let i = 0; i < THRESHOLD_RATIOS.length; i++) {
    if (ratio >= THRESHOLD_RATIOS[i]) return i
  }

  return THRESHOLD_RATIOS.length - 1
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

/**
 * MGas/s range of a step, for the legend. The step holds every value
 * from its own multiple of the threshold up to the next one, e.g.
 * "90 - 120" for step 3 at a threshold of 60.
 */
export function thresholdStepRange(step: number, threshold: number): string {
  const low = THRESHOLD_RATIOS[step] * threshold
  const fmt = (value: number) => (value >= 10 ? value.toFixed(0) : value.toFixed(1))
  if (step === 0) return `≥ ${fmt(low)}`

  const high = THRESHOLD_RATIOS[step - 1] * threshold
  if (low === 0) return `< ${fmt(high)}`

  return `${fmt(low)} – ${fmt(high)}`
}

/**
 * Payload-duration range of a step, for the legend. A long duration is a
 * bad one, so the step bounds invert: step 0 is the shortest.
 */
export function durationStepRange(step: number, slowMs: number): string {
  const limitNs = slowMs * 1_000_000
  const high = THRESHOLD_RATIOS[step] === 0 ? undefined : limitNs / THRESHOLD_RATIOS[step]
  if (step === 0) return `≤ ${formatDuration(high as number)}`

  const low = limitNs / THRESHOLD_RATIOS[step - 1]
  if (high === undefined) return `> ${formatDuration(low)}`

  return `${formatDuration(low)} – ${formatDuration(high)}`
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

/** Map a payload duration to a colour, scaled against the slow limit. */
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
