import { parseEESTName } from './eestName'

/**
 * How a performance heatmap splits its tests into blocks:
 *   - none:      one block with every test.
 *   - benchmark: one block per benchmark gas value from the test name
 *                (e.g. `benchmark-gas-value_60M`), as the filter facets show.
 *   - gas:       one block per gas the test really used, in 1M steps. This
 *                can differ from the name, e.g. a test whose block does less
 *                work than its benchmark value.
 */
export type HeatmapGroupMode = 'none' | 'benchmark' | 'gas'

export interface GasGroup {
  /** Stable key of the block. */
  key: string
  /** Block header, e.g. "60M gas". */
  label: string
  /** Gas of the block, for the order (most gas first). -1 for an unknown gas. */
  gas: number
}

const UNKNOWN_GAS: GasGroup = { key: 'unknown', label: 'Unknown gas', gas: -1 }
const GAS_UNITS: Record<string, number> = { K: 1e3, M: 1e6, G: 1e9 }
const GAS_STEP = 1_000_000

/** Reads a heatmapGroup URL value; anything else is 'none'. */
export function parseHeatmapGroupMode(value: unknown): HeatmapGroupMode {
  return value === 'benchmark' || value === 'gas' ? value : 'none'
}

/** The block of a test by the benchmark gas in its name. */
export function benchmarkGasGroup(testName: string): GasGroup {
  const value = parseEESTName(testName).benchmark
  const match = value?.match(/^(\d+)([KMG]?)$/)
  if (!value || !match) return UNKNOWN_GAS
  return { key: `b:${value}`, label: `${value} gas`, gas: Number(match[1]) * (GAS_UNITS[match[2]] ?? 1) }
}

/** The block of a test by the gas it really used, rounded to 1M. */
export function gasUsedGroup(gasUsed: number | undefined): GasGroup {
  if (gasUsed === undefined || gasUsed <= 0) return UNKNOWN_GAS
  const millions = Math.round(gasUsed / GAS_STEP)
  if (millions === 0) return { key: 'g:<1', label: '<1M gas', gas: GAS_STEP / 2 }
  return { key: `g:${millions}`, label: `${millions}M gas`, gas: millions * GAS_STEP }
}

/**
 * Splits `items` (already in display order) into gas blocks, most gas first;
 * each block keeps the order of `items`.
 */
export function groupByGas<T>(items: T[], groupOf: (item: T) => GasGroup): { group: GasGroup; items: T[] }[] {
  const blocks = new Map<string, { group: GasGroup; items: T[] }>()
  for (const item of items) {
    const group = groupOf(item)
    const block = blocks.get(group.key) ?? { group, items: [] }
    block.items.push(item)
    blocks.set(group.key, block)
  }
  return [...blocks.values()].sort((a, b) => b.group.gas - a.group.gas)
}
