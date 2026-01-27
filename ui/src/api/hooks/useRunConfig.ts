import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { RunConfig } from '../types'

export function useRunConfig(runId: string) {
  return useQuery({
    queryKey: ['run', runId, 'config'],
    queryFn: async () => {
      const { data, status } = await fetchData<RunConfig>(`runs/${runId}/config.json`)
      if (!data) {
        throw new Error(`Failed to fetch run config: ${status}`)
      }
      return data
    },
    enabled: !!runId,
  })
}
