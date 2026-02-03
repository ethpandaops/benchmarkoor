import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { BlockLogs } from '../types'

export function useBlockLogs(runId: string) {
  return useQuery({
    queryKey: ['run', runId, 'block-logs'],
    queryFn: async () => {
      const { data, status } = await fetchData<BlockLogs>(`runs/${runId}/result.block-logs.json`)
      if (!data) {
        // Return null for 404 (not all runs have block logs)
        if (status === 404) return null
        throw new Error(`Failed to fetch block logs: ${status}`)
      }
      return data
    },
    enabled: !!runId,
  })
}
