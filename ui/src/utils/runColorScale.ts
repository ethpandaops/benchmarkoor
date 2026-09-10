// Colour scale for the run heatmaps on the suite page.
//
// The colour of a run says how far it sits below the best run it is
// compared against. A percentile rank cannot say that — it spends the
// whole green-to-red ramp on any set, so twenty runs within 1% of each
// other read as a full rainbow.
//
// The scale saturates at MIN_DROP below the best. A set that genuinely
// spreads further widens the scale to fit, so a suite that mixes clients
// of very different speed still uses every colour.

import { THRESHOLD_COLORS } from './perfThreshold'

// Green to red, shared with the run-detail heatmaps so every heatmap
// in the UI reads the same way.
export const COLORS = THRESHOLD_COLORS
export const LEVELS = COLORS.length

// Colour of a cell whose value is unknown — the middle of the scale.
export const NEUTRAL_COLOR = COLORS[Math.floor(LEVELS / 2)]

// Shortfall that reaches the end of the scale, unless the runs spread
// wider than this on their own.
export const MIN_DROP = 0.15

export interface ColorScale {
  color: (value: number) => string
  // Reference value the scale measures from — the robust best of the
  // set. Every run at or above it takes the first colour.
  top: number
  // Width of one colour step, as a fraction of `top`.
  step: number
  hasData: boolean
}

const EMPTY_SCALE: ColorScale = {
  color: () => NEUTRAL_COLOR,
  top: 0,
  step: MIN_DROP / LEVELS,
  hasData: false,
}

/** Percentile of an already sorted list. */
export function calculatePercentile(sortedValues: number[], percentile: number): number {
  if (sortedValues.length === 0) return 0
  const index = (percentile / 100) * (sortedValues.length - 1)
  const lower = Math.floor(index)
  const upper = Math.ceil(index)
  if (lower === upper) return sortedValues[lower]

  return sortedValues[lower] + (sortedValues[upper] - sortedValues[lower]) * (index - lower)
}

/**
 * Create a colour mapper that grades runs by their shortfall against the
 * best of the set. The ends are percentiles, not the raw min and max, so
 * one flukey run cannot compress the scale for every other run.
 */
export function createColorScale(values: number[], higherIsBetter: boolean): ColorScale {
  if (values.length === 0) return EMPTY_SCALE

  const sorted = [...values].sort((a, b) => a - b)
  const top = calculatePercentile(sorted, higherIsBetter ? 90 : 10)
  const low = calculatePercentile(sorted, higherIsBetter ? 10 : 90)
  if (top <= 0) return EMPTY_SCALE

  const step = Math.max(MIN_DROP, Math.abs(low - top) / top) / LEVELS

  const color = (value: number) => {
    const shortfall = higherIsBetter ? (top - value) / top : (value - top) / top
    if (shortfall <= 0) return COLORS[0]

    return COLORS[Math.min(LEVELS - 1, Math.floor(shortfall / step))]
  }

  return { color, top, step, hasData: true }
}

/** Render a shortfall fraction as a percentage, e.g. 0.015 -> "1.5%". */
export function formatShortfall(fraction: number): string {
  const percent = fraction * 100
  return `${percent < 10 ? percent.toFixed(1) : percent.toFixed(0)}%`
}
