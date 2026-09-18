import { describe, expect, it } from 'vitest'
import { encodeGroupsParam, parseGroupsParam, selectGroupRuns, setGroupRuns, toggleGroupRun } from './groupUtils'

const matched = [{ run_id: 'r5' }, { run_id: 'r4' }, { run_id: 'r3' }, { run_id: 'r2' }, { run_id: 'r1' }]

describe('parseGroupsParam / encodeGroupsParam', () => {
  it('round-trips groups without a run selection', () => {
    const param = 'geth:image=v1,net=a;reth:'
    const groups = parseGroupsParam(param)
    expect(groups).toEqual([
      { client: 'geth', metadata: { image: 'v1', net: 'a' } },
      { client: 'reth', metadata: {} },
    ])
    expect(encodeGroupsParam(groups)).toBe(param)
  })

  it('round-trips an explicit run selection', () => {
    const param = 'geth:image=v1|r5,r3;reth:|r1'
    const groups = parseGroupsParam(param)
    expect(groups).toEqual([
      { client: 'geth', metadata: { image: 'v1' }, runs: ['r5', 'r3'] },
      { client: 'reth', metadata: {}, runs: ['r1'] },
    ])
    expect(encodeGroupsParam(groups)).toBe(param)
  })

  it('treats an empty run list as an explicit empty selection', () => {
    expect(parseGroupsParam('geth:|')).toEqual([{ client: 'geth', metadata: {}, runs: [] }])
  })
})

describe('selectGroupRuns', () => {
  it('takes the newest sampleSize runs for an automatic group', () => {
    expect(selectGroupRuns({ client: 'geth', metadata: {} }, matched, 2)).toEqual([{ run_id: 'r5' }, { run_id: 'r4' }])
  })

  it('takes exactly the selected runs, in matched order, for a manual group', () => {
    const group = { client: 'geth', metadata: {}, runs: ['r1', 'r4', 'gone'] }
    expect(selectGroupRuns(group, matched, 2)).toEqual([{ run_id: 'r4' }, { run_id: 'r1' }])
  })
})

describe('toggleGroupRun', () => {
  it('materialises the sample and removes the clicked run', () => {
    const next = toggleGroupRun({ client: 'geth', metadata: {} }, matched, 3, 'r4')
    expect(next.runs).toEqual(['r5', 'r3'])
  })

  it('materialises the sample and adds a run outside it', () => {
    const next = toggleGroupRun({ client: 'geth', metadata: {} }, matched, 2, 'r1')
    expect(next.runs).toEqual(['r5', 'r4', 'r1'])
  })

  it('drops back to automatic when the selection equals the sample again', () => {
    const manual = { client: 'geth', metadata: { a: 'b' }, runs: ['r5', 'r3'] }
    const next = toggleGroupRun(manual, matched, 3, 'r4')
    expect(next).toEqual({ client: 'geth', metadata: { a: 'b' } })
    expect('runs' in next).toBe(false)
  })

  it('allows an empty manual selection', () => {
    const next = toggleGroupRun({ client: 'geth', metadata: {}, runs: ['r5'] }, matched, 1, 'r5')
    expect(next.runs).toEqual([])
  })
})

describe('setGroupRuns', () => {
  it('includes a range on top of the sample', () => {
    const next = setGroupRuns({ client: 'geth', metadata: {} }, matched, 1, ['r3', 'r2'], true)
    expect(next.runs).toEqual(['r5', 'r3', 'r2'])
  })

  it('excludes a range from a manual selection', () => {
    const manual = { client: 'geth', metadata: {}, runs: ['r5', 'r4', 'r3', 'r2'] }
    const next = setGroupRuns(manual, matched, 5, ['r4', 'r3'], false)
    expect(next.runs).toEqual(['r5', 'r2'])
  })

  it('drops back to automatic when a range restores the sample', () => {
    const manual = { client: 'geth', metadata: {}, runs: ['r5'] }
    const next = setGroupRuns(manual, matched, 3, ['r4', 'r3'], true)
    expect('runs' in next).toBe(false)
  })
})
