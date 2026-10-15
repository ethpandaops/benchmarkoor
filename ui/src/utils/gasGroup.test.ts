import { describe, expect, it } from 'vitest'
import { benchmarkGasGroup, gasUsedGroup, groupByGas, parseHeatmapGroupMode } from './gasGroup'

const name = (gas: string) =>
  `benchmark/compute/instruction/test_arithmetic.py::test_arithmetic[fork_Amsterdam-blockchain_test_stateful_engine-opcode_ADD-benchmark-gas-value_${gas}]`

describe('parseHeatmapGroupMode', () => {
  it('keeps the known modes and falls back to none', () => {
    expect(parseHeatmapGroupMode('benchmark')).toBe('benchmark')
    expect(parseHeatmapGroupMode('gas')).toBe('gas')
    expect(parseHeatmapGroupMode(undefined)).toBe('none')
    expect(parseHeatmapGroupMode(60)).toBe('none')
  })
})

describe('benchmarkGasGroup', () => {
  it('reads the gas value of the test name', () => {
    expect(benchmarkGasGroup(name('60M'))).toEqual({ key: 'b:60M', label: '60M gas', gas: 60_000_000 })
    expect(benchmarkGasGroup(name('1G')).gas).toBe(1_000_000_000)
  })

  it('is unknown without a gas value', () => {
    expect(benchmarkGasGroup('some/test.txt').gas).toBe(-1)
  })
})

describe('gasUsedGroup', () => {
  it('rounds the real gas to 1M', () => {
    expect(gasUsedGroup(59_997_000)).toEqual({ key: 'g:60', label: '60M gas', gas: 60_000_000 })
    expect(gasUsedGroup(52_300_000).label).toBe('52M gas')
  })

  it('keeps a test far below its benchmark value apart', () => {
    expect(gasUsedGroup(23_048).label).toBe('<1M gas')
  })

  it('is unknown without gas', () => {
    expect(gasUsedGroup(0).gas).toBe(-1)
    expect(gasUsedGroup(undefined).gas).toBe(-1)
  })
})

describe('groupByGas', () => {
  it('puts the most gas first and keeps the item order inside a block', () => {
    const items = [
      { id: 'a', gas: 10_000_000 },
      { id: 'b', gas: 300_000_000 },
      { id: 'c', gas: 10_200_000 },
      { id: 'd', gas: 0 },
    ]
    const blocks = groupByGas(items, (i) => gasUsedGroup(i.gas))
    expect(blocks.map((b) => [b.group.label, b.items.map((i) => i.id)])).toEqual([
      ['300M gas', ['b']],
      ['10M gas', ['a', 'c']],
      ['Unknown gas', ['d']],
    ])
  })
})
