import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { RunConfig } from '../types'

export function useRunConfig(runId: string) {
  return useQuery({
    queryKey: ['run', runId, 'config'],
    queryFn: () => fetchData<RunConfig>(`runs/${runId}/config.json`),
    enabled: !!runId,
  })
}
