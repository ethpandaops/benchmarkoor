import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { StateActorManifest } from '../types'

// useStateActorManifest fetches a run's state-actor provenance manifest. It is
// optional — only present when the run's snapshot was built by a state-actor
// that emits it — so a 404 resolves to null instead of erroring.
export function useStateActorManifest(runId: string, enabled = true) {
  return useQuery({
    queryKey: ['run', runId, 'state-actor-manifest'],
    queryFn: async () => {
      const { data, status } = await fetchData<StateActorManifest>(
        `runs/${runId}/.state-actor/state-actor-manifest.json`,
      )
      if (!data) {
        if (status === 404) return null
        throw new Error(`Failed to fetch state-actor manifest: ${status}`)
      }
      return data
    },
    enabled: !!runId && enabled,
  })
}
