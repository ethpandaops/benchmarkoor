import type { IndexEntry } from '@/api/types'

// The runs a run-detail page compares its run against: the other runs
// of the same suite by the same client.
//
// Runs that carry the same labels are the best peers, so those win when
// there are any. A run whose labels match no other run still needs a
// strip, so the set widens to every run of the client in that case
// instead of hiding the strip.
export function selectClientPeerRuns(
  entries: IndexEntry[],
  suiteHash: string | undefined,
  client: string | undefined,
  labels: Record<string, string> | undefined,
): IndexEntry[] {
  if (!suiteHash || !client) return []

  const sameClient = entries.filter(
    (r) => r.suite_hash === suiteHash && r.instance.client === client,
  )

  const sameLabels = sameClient.filter((r) => hasSameLabels(r.metadata, labels))

  return sameLabels.length > 1 ? sameLabels : sameClient
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
