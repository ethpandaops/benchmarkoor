import type { ProcessedTestData, BoxPlotStats, TestCategory } from '../types'

/**
 * Calculate a specific percentile from a sorted array of numbers.
 */
export function percentile(sortedValues: number[], p: number): number {
  if (sortedValues.length === 0) return 0
  if (sortedValues.length === 1) return sortedValues[0]

  const index = (p / 100) * (sortedValues.length - 1)
  const lower = Math.floor(index)
  const upper = Math.ceil(index)
  const weight = index - lower

  if (upper >= sortedValues.length) return sortedValues[sortedValues.length - 1]

  return sortedValues[lower] * (1 - weight) + sortedValues[upper] * weight
}

/**
 * Calculate IQR (Interquartile Range) boundaries for outlier detection.
 */
export function calculateIQRBounds(values: number[]): { lower: number; upper: number } {
  if (values.length < 4) {
    return { lower: -Infinity, upper: Infinity }
  }

  const sorted = [...values].sort((a, b) => a - b)
  const q1 = percentile(sorted, 25)
  const q3 = percentile(sorted, 75)
  const iqr = q3 - q1

  return {
    lower: q1 - 1.5 * iqr,
    upper: q3 + 1.5 * iqr,
  }
}

/**
 * Remove outliers from data using IQR method.
 */
export function removeOutliers<T extends ProcessedTestData>(
  data: T[],
  getValue: (item: T) => number
): T[] {
  const values = data.map(getValue)
  const { lower, upper } = calculateIQRBounds(values)

  return data.filter((item) => {
    const value = getValue(item)
    return value >= lower && value <= upper
  })
}

/**
 * Calculate box plot statistics for each category.
 */
export function calculateBoxPlotStats(data: ProcessedTestData[]): BoxPlotStats[] {
  const categories: TestCategory[] = ['add', 'mul', 'pairing', 'other']
  const result: BoxPlotStats[] = []

  for (const category of categories) {
    const categoryData = data.filter((d) => d.category === category)
    if (categoryData.length === 0) continue

    const values = categoryData.map((d) => d.throughput).sort((a, b) => a - b)
    const q1 = percentile(values, 25)
    const q3 = percentile(values, 75)
    const iqr = q3 - q1
    const lowerBound = q1 - 1.5 * iqr
    const upperBound = q3 + 1.5 * iqr

    const nonOutliers = values.filter((v) => v >= lowerBound && v <= upperBound)
    const outliers = values.filter((v) => v < lowerBound || v > upperBound)

    result.push({
      category,
      min: nonOutliers.length > 0 ? nonOutliers[0] : values[0],
      q1,
      median: percentile(values, 50),
      q3,
      max: nonOutliers.length > 0 ? nonOutliers[nonOutliers.length - 1] : values[values.length - 1],
      outliers,
    })
  }

  return result
}

/**
 * Create histogram bins for throughput distribution.
 */
export function createHistogramBins(
  data: ProcessedTestData[],
  binCount: number = 20
): { start: number; end: number; count: number; byCategory: Record<TestCategory, number> }[] {
  if (data.length === 0) return []

  const values = data.map((d) => d.throughput)
  const min = Math.min(...values)
  const max = Math.max(...values)

  if (min === max) {
    return [{
      start: min,
      end: max,
      count: data.length,
      byCategory: {
        add: data.filter((d) => d.category === 'add').length,
        mul: data.filter((d) => d.category === 'mul').length,
        pairing: data.filter((d) => d.category === 'pairing').length,
        other: data.filter((d) => d.category === 'other').length,
      },
    }]
  }

  const binWidth = (max - min) / binCount
  const bins: { start: number; end: number; count: number; byCategory: Record<TestCategory, number> }[] = []

  for (let i = 0; i < binCount; i++) {
    bins.push({
      start: min + i * binWidth,
      end: min + (i + 1) * binWidth,
      count: 0,
      byCategory: { add: 0, mul: 0, pairing: 0, other: 0 },
    })
  }

  for (const item of data) {
    let binIndex = Math.floor((item.throughput - min) / binWidth)
    if (binIndex >= binCount) binIndex = binCount - 1
    if (binIndex < 0) binIndex = 0

    bins[binIndex].count++
    bins[binIndex].byCategory[item.category]++
  }

  return bins
}

/**
 * Normalize a value to 0-100 scale based on min/max range.
 * Higher values = better for throughput and speed.
 * Lower values = better for overhead (so we invert it).
 */
export function normalizeValue(
  value: number,
  min: number,
  max: number,
  invert: boolean = false
): number {
  if (max === min) return 50

  const normalized = ((value - min) / (max - min)) * 100
  return invert ? 100 - normalized : normalized
}
