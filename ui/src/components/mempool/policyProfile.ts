import type { MempoolRunResult, MempoolScenarioResult } from '@/api/types'
import { classifyObservation, formatSig } from './mempoolUtils'

// ProfileField is one line of a client policy profile. value is undefined
// when the run has no data for it (an older run, or a scenario that did not
// run); the UI shows "—".
export interface ProfileField {
  key: string
  label: string
  hint: string
  value?: string
}

// LIMIT_MODELS says, per client, how its pool counts against its limits.
// It is static knowledge of the client code, not a measurement.
export const LIMIT_MODELS: Record<string, string> = {
  geth: 'counts txs; account slots are a guarantee',
  besu: 'limits by memory (layered pool), not by tx count',
  nethermind: 'TxPool.Size total; no per-account queue cap',
  reth: 'per-sender slots are a hard cap',
  erigon: 'pending/base-fee/queued sub-pools with separate caps',
}

export function limitModel(client: string): string | undefined {
  return LIMIT_MODELS[client.toLowerCase()]
}

function scenario(r: MempoolRunResult, name: string): MempoolScenarioResult | undefined {
  return (r.scenarios ?? []).find((s) => s.name === name)
}

function check(r: MempoolRunResult, name: string): boolean | undefined {
  for (const s of r.scenarios ?? []) {
    const a = (s.assertions ?? []).find((x) => x.name === name)
    if (a) return a.passed
  }
  return undefined
}

function observation(r: MempoolRunResult, scenarioName: string, key: string): string | undefined {
  return scenario(r, scenarioName)?.observations?.find((o) => o.key === key)?.value
}

function metric(r: MempoolRunResult, scenarioName: string, key: string): number | undefined {
  const v = scenario(r, scenarioName)?.metrics?.find((m) => m.key === key)?.value
  return typeof v === 'number' && Number.isFinite(v) ? v : undefined
}

function numParam(s: MempoolScenarioResult | undefined, key: string): number | undefined {
  const v = s?.params?.[key]
  return typeof v === 'number' && Number.isFinite(v) ? v : undefined
}

function yesNo(v: boolean | undefined): string | undefined {
  return v === undefined ? undefined : v ? 'yes' : 'no'
}

function ofTotal(v: number | undefined, total: number | undefined): string | undefined {
  if (v === undefined) return undefined
  return total === undefined ? formatSig(v) : `${formatSig(v)} of ${formatSig(total)}`
}

// The checks and observations the profile reads. The harness names them;
// keep them in step with pkg/mempool/scenario.
export const PROFILE_SOURCES = {
  replacementAtBump: 'Replacement: Fee cap and tip +10%',
  dropsUnpayable: 'The pool drops txs whose sender was emptied',
  blobReinject: 'Blob txs of an orphaned block come back to the pool',
  cumulativeValue: "The pool counts value across a sender's txs (5 ETH after 8 ETH)",
} as const

// buildPolicyProfile derives the pool policy of a client from one run.
export function buildPolicyProfile(r: MempoolRunResult): ProfileField[] {
  const eviction = scenario(r, 'eviction-limits')
  const atBump = check(r, PROFILE_SOURCES.replacementAtBump)
  const hogTotal = numParam(eviction, 'hog_txs') ?? (eviction ? 256 : undefined)
  const queueExtra = numParam(eviction, 'queue_extra')
  const accountQueue = r.pool_limits?.account_queue
  const queueTotal = queueExtra !== undefined && accountQueue !== undefined ? accountQueue + queueExtra : eviction ? 80 : undefined
  const perSender = observation(r, 'blob-pool', 'per_sender')
  const perSenderNum = perSender !== undefined ? /^\s*(\d+)/.exec(perSender)?.[1] : undefined
  const delegated = metric(r, 'setcode-delegation', 'inflight.delegated')
  const farNonce = observation(r, 'nonce-gap-promotion', 'far_nonce')
  const zeroTip = observation(r, 'base-fee-movement', 'zero_tip.included')?.trim().toLowerCase()
  const v0 = observation(r, 'admission-rejections', 'reject.blob_v0_sidecar')

  return [
    {
      key: 'replacement_bump',
      label: 'Replacement bump',
      hint: 'The fee increase a replacement needs. "≥ 10%" accepts exactly +10% on fee cap and tip; "> 10%" needs more.',
      value: atBump === undefined ? undefined : atBump ? '≥ 10%' : '> 10%',
    },
    {
      key: 'per_sender',
      label: 'Txs kept per sender',
      hint: 'Pending txs the pool kept from one sender that sent many into a full pool (eviction-limits, hog.kept).',
      value: ofTotal(metric(r, 'eviction-limits', 'hog.kept'), hogTotal),
    },
    {
      key: 'queue_per_account',
      label: 'Future txs per account',
      hint: 'Gapped (future-nonce) txs the pool kept from one account (eviction-limits, queue.kept).',
      value: ofTotal(metric(r, 'eviction-limits', 'queue.kept'), queueTotal),
    },
    {
      key: 'blob_per_sender',
      label: 'Blob txs per sender',
      hint: 'Blob txs the pool accepted from one sender (blob-pool, per_sender).',
      value: perSenderNum,
    },
    {
      key: 'delegated_inflight',
      label: 'Delegated in-flight',
      hint: 'Pending txs the pool allows for an account with a 7702 delegation (setcode-delegation, inflight.delegated).',
      value: delegated === undefined ? undefined : formatSig(delegated),
    },
    {
      key: 'far_nonce',
      label: 'Far-future nonce',
      hint: 'What the pool does with a tx whose nonce is far past the account nonce (nonce-gap-promotion, far_nonce).',
      value: farNonce === undefined ? undefined : classifyObservation(farNonce) === 'accepted' ? 'queued' : 'rejected',
    },
    {
      key: 'zero_tip',
      label: 'Zero-tip built',
      hint: 'Whether the builder includes a tx with a zero priority fee (base-fee-movement, zero_tip.included).',
      value: zeroTip === 'true' ? 'yes' : zeroTip === 'false' ? 'no' : undefined,
    },
    {
      key: 'drops_unpayable',
      label: 'Drops unpayable txs',
      hint: 'Whether the pool removes pending txs after their sender lost its balance (balance-changes).',
      value: yesNo(check(r, PROFILE_SOURCES.dropsUnpayable)),
    },
    {
      key: 'blob_reinject',
      label: 'Reinjects blob txs',
      hint: 'Whether blob txs of an orphaned block come back to the pool after a reorg (blob-pool).',
      value: yesNo(check(r, PROFILE_SOURCES.blobReinject)),
    },
    {
      key: 'cumulative_value',
      label: 'Counts value across txs',
      hint: "Whether the pool checks the sum of a sender's pending tx values against the balance (nonce-gap-promotion).",
      value: yesNo(check(r, PROFILE_SOURCES.cumulativeValue)),
    },
    {
      key: 'blob_v0',
      label: 'v0 blob sidecar',
      hint: 'What the pool does with a blob tx that has a version 0 sidecar after Osaka (admission-rejections, reject.blob_v0_sidecar).',
      value: v0 === undefined ? undefined : classifyObservation(v0) === 'accepted' ? 'converts' : 'rejects',
    },
  ]
}

// PolicyRow is one policy field across clients, for the client comparison.
export interface PolicyRow {
  key: string
  label: string
  hint: string
  // values holds the value of each client, by client name.
  values: Record<string, string | undefined>
  // differs is true when two or more clients have different known values.
  differs: boolean
}

// comparePolicies lays the policy profiles of several clients side by side.
// The first row is the limit model; it is static, so it never differs.
export function comparePolicies(results: { client: string; result: MempoolRunResult }[]): PolicyRow[] {
  const profiles = results.map((r) => ({ client: r.client, fields: buildPolicyProfile(r.result) }))
  const rows: PolicyRow[] = [
    {
      key: 'limit_model',
      label: 'Limit model',
      hint: 'How the client limits its pool (from the client code, not measured).',
      values: Object.fromEntries(results.map((r) => [r.client, limitModel(r.client)])),
      differs: false,
    },
  ]
  for (const [i, field] of (profiles[0]?.fields ?? []).entries()) {
    const values = Object.fromEntries(profiles.map((p) => [p.client, p.fields[i]?.value]))
    const known = new Set(Object.values(values).filter((v) => v !== undefined))
    rows.push({ key: field.key, label: field.label, hint: field.hint, values, differs: known.size > 1 })
  }
  return rows
}
