import { describe, it, expect } from 'vitest'
import type { MempoolRunResult } from '@/api/types'
import v2Fixture from './__fixtures__/mempool-result.v2.json'
import gethFixture from './__fixtures__/mempool-result.geth.json'
import { collectProblems, filterScenarios, scenarioDomId, scenarioHeadlines, summarizeInvariants } from './runOverview'
import { buildIssueMarkdown, cellResultText, clientVersion } from './issueMarkdown'

const v2 = v2Fixture as unknown as MempoolRunResult
const old = gethFixture as unknown as MempoolRunResult

describe('scenarioDomId', () => {
  it('makes a safe id', () => {
    expect(scenarioDomId('blob-pool')).toBe('scenario-blob-pool')
    expect(scenarioDomId('a b/c')).toBe('scenario-a-b-c')
  })
})

describe('collectProblems', () => {
  it('lists hard failures and scenario errors', () => {
    const p = collectProblems(v2.scenarios)
    expect(p.map((x) => [x.scenario, x.kind])).toEqual([
      ['replacement', 'hard-fail'],
      ['eviction-limits', 'hard-fail'],
      ['balance-changes', 'error'],
    ])
    expect(p[0].actual).toBe('original gone')
  })
  it('is empty for a clean run', () => {
    expect(collectProblems(old.scenarios.filter((x) => x.status === 'passed'))).toEqual([])
  })
})

describe('summarizeInvariants', () => {
  const inv = summarizeInvariants(v2.scenarios)
  it('has one entry per invariant name, failed first', () => {
    expect(new Set(inv.map((i) => i.name)).size).toBe(inv.length)
    expect(inv[0].name).toBe('Invariant: Included txs leave the pool')
    expect(inv[0].failed).toBe(1)
    expect(inv[0].failures[0]).toMatchObject({ scenario: 'eviction-limits', actual: '2 txs still pending' })
    expect(inv.slice(1).every((i) => i.failed === 0)).toBe(true)
  })
  it('counts the scenarios', () => {
    const i = inv.find((x) => x.name === 'Invariant: Every built payload is valid (newPayload)')!
    expect(i.scenarios.length).toBeGreaterThan(1)
    expect(i.passed).toBeGreaterThanOrEqual(i.scenarios.length)
  })
})

describe('filterScenarios', () => {
  it('keeps everything with "all"', () => {
    expect(filterScenarios(v2.scenarios, 'all', false)).toHaveLength(v2.scenarios.length)
  })
  it('keeps only failed checks with "problems"', () => {
    const f = filterScenarios(v2.scenarios, 'problems', false)
    expect(f.every((x) => x.observations.length === 0)).toBe(true)
    expect(f.every((x) => x.assertions.every((a) => !a.passed))).toBe(true)
    expect(f.map((x) => x.scenario.name)).toContain('balance-changes')
  })
  it('hides passing scenarios', () => {
    const f = filterScenarios(v2.scenarios, 'all', true)
    expect(f.every((x) => x.scenario.status !== 'passed' || x.scenario.assertions.some((a) => !a.passed))).toBe(true)
  })
})

describe('scenarioHeadlines', () => {
  it('formats metric and latency headlines', () => {
    const ev = v2.scenarios.find((s) => s.name === 'eviction-limits')!
    expect(scenarioHeadlines(ev)).toEqual([{ label: expect.any(String), text: '100%' }])
    const rep = v2.scenarios.find((s) => s.name === 'replacement')!
    const h = scenarioHeadlines(rep)
    expect(h).toHaveLength(1)
    expect(h[0].label).toContain('(p50)')
    expect(h[0].text).toMatch(/ms|µs|s$/)
  })
  it('returns nothing for an unknown scenario', () => {
    expect(scenarioHeadlines({ ...v2.scenarios[0], name: 'unknown' })).toEqual([])
  })
})

describe('issue markdown', () => {
  it('renders the title, explanation, table and params', () => {
    const md = buildIssueMarkdown({
      title: 'Replacement: Fee cap and tip +10%',
      scenario: 'replacement',
      kind: 'check',
      severity: 'soft',
      explain: { what: 'W', why: 'Y', ok: 'O' },
      rows: [
        { client: 'geth', runId: 'r1', version: 'Geth/v1', result: cellResultText({ kind: 'check', state: 'passed' }) },
        { client: 'reth', runId: 'r2', result: cellResultText({ kind: 'check', state: 'soft-fail', expected: 'a|b', actual: 'c' }) },
      ],
      params: { timeout_ms: 10000 },
    })
    expect(md).toContain('## Replacement: Fee cap and tip +10%')
    expect(md).toContain('**What it tests:** W')
    expect(md).toContain('| geth | passed | Geth/v1 | `r1` |')
    expect(md).toContain('| reth | finding (soft check failed): expected a\\|b, actual c | - | `r2` |')
    expect(md).toContain('- `timeout_ms`: `10000`')
  })
  it('prefers the client version over the image', () => {
    expect(clientVersion({ instance: { client_version: 'v1', image: 'img' } as never })).toBe('v1')
    expect(clientVersion({ instance: { image: 'img' } as never })).toBe('img')
    expect(clientVersion(undefined)).toBeUndefined()
  })
})

describe('blocks and fees', async () => {
  const { blockAt, feeUnit } = await import('./mempoolUtils')
  const blocks = [{ t_ms: 10 }, { t_ms: 20 }, { t_ms: 30 }]
  it('finds the block at a time', () => {
    expect(blockAt(blocks, 5)).toBeUndefined()
    expect(blockAt(blocks, 10)).toBe(blocks[0])
    expect(blockAt(blocks, 25)).toBe(blocks[1])
    expect(blockAt(blocks, 99)).toBe(blocks[2])
    expect(blockAt(undefined, 1)).toBeUndefined()
  })
  it('picks a fee unit', () => {
    expect(feeUnit(7e9).unit).toBe('gwei')
    expect(feeUnit(12).unit).toBe('wei')
  })
})
