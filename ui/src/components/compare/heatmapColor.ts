// Colour model of the group heatmap, shared with the test detail modal
// so its cards match the tiles. Separated from the component file so
// the React fast-refresh lint rule doesn't complain about mixed exports.

import { THRESHOLD_COLORS, getColorByThreshold, thresholdStep } from '@/utils/perfThreshold'

// What a tile says: throughput against an absolute threshold, or the
// ratio to the baseline group on the same test.
export type HeatmapColorMode = 'mgas' | 'baseline'

/**
 * Colour of one group's value for one test, or undefined when there is
 * no value to colour (the caller draws the no-data tile).
 */
export function heatmapColor(
  value: number | undefined,
  baseValue: number | undefined,
  mode: HeatmapColorMode,
  threshold: number,
): string | undefined {
  if (value === undefined) return undefined
  if (mode === 'mgas') return getColorByThreshold(value, threshold)
  if (baseValue === undefined) return undefined

  return THRESHOLD_COLORS[thresholdStep(value / baseValue)]
}

/** Render a ratio to the baseline as a signed percentage, e.g. 1.2 -> "+20%". */
export function formatRatio(ratio: number): string {
  const percent = (ratio - 1) * 100
  const sign = percent > 0 ? '+' : ''
  return `${sign}${Math.abs(percent) < 10 ? percent.toFixed(1) : percent.toFixed(0)}%`
}
