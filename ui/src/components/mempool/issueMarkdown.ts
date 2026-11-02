import type { MempoolCheckExplanation, RunConfig } from '@/api/types'
import type { BehaviourCell } from './behaviours'
import { formatParam, type CheckState } from './mempoolUtils'

export interface IssueClientRow {
  client: string
  runId: string
  result: string
  version?: string
}

export interface IssueInput {
  title: string
  scenario: string
  kind: 'check' | 'observation'
  severity?: 'hard' | 'soft'
  explain?: MempoolCheckExplanation
  rows: IssueClientRow[]
  params?: Record<string, unknown>
}

const STATE_TEXT: Record<CheckState, string> = {
  passed: 'passed',
  'soft-fail': 'finding (soft check failed)',
  'hard-fail': 'FAILED (hard check)',
}

// cellResultText is the result of one client as plain text.
export function cellResultText(cell: BehaviourCell | undefined): string {
  if (!cell) return '-'
  if (cell.kind === 'observation') return cell.value
  const parts = [STATE_TEXT[cell.state]]
  if (cell.state !== 'passed' && (cell.expected !== undefined || cell.actual !== undefined)) {
    parts.push(`expected ${cell.expected ?? '-'}, actual ${cell.actual ?? '-'}`)
  }
  return parts.join(': ')
}

// clientVersion is the version a run reports, else its image.
export function clientVersion(config: Pick<RunConfig, 'instance'> | undefined | null): string | undefined {
  return config?.instance?.client_version || config?.instance?.image || undefined
}

function cell(text: string | undefined): string {
  if (!text) return '-'
  return text.replace(/\|/g, '\\|').replace(/\r?\n/g, ' ')
}

// buildIssueMarkdown renders a check or observation as a Markdown issue
// body: title, explanation, one table row per client and the scenario
// parameters.
export function buildIssueMarkdown(input: IssueInput): string {
  const lines: string[] = [`## ${input.title}`, '']
  const meta = [`Scenario: \`${input.scenario}\``]
  if (input.kind === 'check') meta.push(`Check: ${input.severity ?? 'soft'}`)
  else meta.push('Observation')
  lines.push(meta.join(' · '), '')

  if (input.explain) {
    const observation = input.kind === 'observation'
    lines.push(
      `**${observation ? 'What it records' : 'What it tests'}:** ${input.explain.what}`,
      '',
      `**Why it matters:** ${input.explain.why}`,
      '',
      `**${observation ? 'How to read it' : 'OK results'}:** ${input.explain.ok}`,
      '',
    )
  }

  lines.push('| Client | Result | Version | Run |', '| --- | --- | --- | --- |')
  for (const r of input.rows) {
    lines.push(`| ${cell(r.client)} | ${cell(r.result)} | ${cell(r.version)} | \`${cell(r.runId)}\` |`)
  }
  lines.push('')

  const params = Object.entries(input.params ?? {})
  if (params.length > 0) {
    lines.push('Scenario parameters:', '')
    for (const [k, v] of params) lines.push(`- \`${k}\`: \`${formatParam(v)}\``)
    lines.push('')
  }
  return lines.join('\n')
}
