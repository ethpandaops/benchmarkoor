// Colour model of the group heatmap, shared with the test detail modal
// so its cards match the tiles. Separated from the component file so
// the React fast-refresh lint rule doesn't complain about mixed exports.

import { THRESHOLD_COLORS, getColorByDuration, getColorByThreshold, thresholdStep } from '@/utils/perfThreshold'

// What a tile measures: throughput, or the total engine_newPayload time
// of the test. Duration is the total, not the slowest single payload the
// run page uses, because the averaged group result keeps no per-call
// stats. The two are the same for a single-block test.
export type HeatmapMetric = 'mgas' | 'duration'

// What a tile says: the value against an absolute limit (a MGas/s
// threshold or a slow-payload limit), or the ratio to the baseline group
// on the same test.
export type HeatmapColorMode = 'absolute' | 'baseline'

export interface HeatmapColorModel {
  metric: HeatmapMetric
  mode: HeatmapColorMode
  /** MGas/s threshold of the absolute mode, for the 'mgas' metric. */
  threshold: number
  /** Slow-payload limit in milliseconds of the absolute mode, for the 'duration' metric. */
  slowMs: number
}

/** Ratio of a value to the baseline where higher is better, so 1.25 means 25% faster. */
export function baselineRatio(value: number, baseValue: number, metric: HeatmapMetric): number {
  return metric === 'mgas' ? value / baseValue : baseValue / value
}

/**
 * Colour of one group's value for one test, or undefined when there is
 * no value to colour (the caller draws the no-data tile).
 */
export function heatmapColor(
  value: number | undefined,
  baseValue: number | undefined,
  model: HeatmapColorModel,
): string | undefined {
  if (value === undefined) return undefined
  if (model.mode === 'absolute') {
    return model.metric === 'mgas' ? getColorByThreshold(value, model.threshold) : getColorByDuration(value, model.slowMs)
  }
  if (baseValue === undefined || baseValue <= 0 || value <= 0) return undefined

  return THRESHOLD_COLORS[thresholdStep(baselineRatio(value, baseValue, model.metric))]
}

/** Render a ratio to the baseline as a signed percentage, e.g. 1.2 -> "+20%". */
export function formatRatio(ratio: number): string {
  const percent = (ratio - 1) * 100
  const sign = percent > 0 ? '+' : ''
  return `${sign}${Math.abs(percent) < 10 ? percent.toFixed(1) : percent.toFixed(0)}%`
}
