import type { IndexEntry } from '@/api/types'

// The runs a run-detail page compares its run against: the other runs
// of the same suite by the same client.
//
// Runs that carry the same labels are the best peers, so those win when
// there are any. Labels often carry per-run values (a CI job id, a
// commit), so a run rarely finds one. The set then falls back to the
// runs of the same instance id, which is how an operator names a
// variant of a client, and last to every run of the client, so the
// strip only hides when the run really has no peer.
export function selectClientPeerRuns(
  entries: IndexEntry[],
  suiteHash: string | undefined,
  client: string | undefined,
  instanceId: string | undefined,
  labels: Record<string, string> | undefined,
): IndexEntry[] {
  if (!suiteHash || !client) return []

  const sameClient = entries.filter(
    (r) => r.suite_hash === suiteHash && r.instance.client === client,
  )

  const sameLabels = sameClient.filter((r) => hasSameLabels(r.metadata, labels))
  if (sameLabels.length > 1) return sameLabels

  const sameInstance = instanceId ? sameClient.filter((r) => r.instance.id === instanceId) : []
  if (sameInstance.length > 1) return sameInstance

  return sameClient
}

// Same labels: same set of keys, same value for each key.
function hasSameLabels(
  a: Record<string, string> | undefined,
  b: Record<string, string> | undefined,
): boolean {
  const aKeys = Object.keys(a ?? {})
  const bKeys = Object.keys(b ?? {})
  if (aKeys.length !== bKeys.length) return false

  return aKeys.every((k) => a?.[k] === b?.[k])
}
