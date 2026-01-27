import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { SuiteStats } from '../types'

export function useSuiteStats(suiteHash: string | undefined) {
  return useQuery({
    queryKey: ['suiteStats', suiteHash],
    queryFn: async () => {
      const { data, status } = await fetchData<SuiteStats>(`suites/${suiteHash}/stats.json`)
      if (!data) {
        throw new Error(`Failed to fetch suite stats: ${status}`)
      }
      return data
    },
    enabled: !!suiteHash,
  })
}
