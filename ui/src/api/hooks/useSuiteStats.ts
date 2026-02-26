import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { SuiteStats } from '../types'
import { loadRuntimeConfig, isIndexingEnabled } from '@/config/runtime'

export function useSuiteStats(suiteHash: string | undefined) {
  return useQuery({
    queryKey: ['suiteStats', suiteHash],
    queryFn: async () => {
      const config = await loadRuntimeConfig()

      if (isIndexingEnabled(config) && config.api?.baseUrl) {
        const response = await fetch(
          `${config.api.baseUrl}/api/v1/index/suites/${suiteHash}/stats`,
          { credentials: 'include' },
        )

        if (!response.ok) {
          throw new Error(`Failed to fetch suite stats: ${response.status}`)
        }

        return response.json() as Promise<SuiteStats>
      }

      const { data, status } = await fetchData<SuiteStats>(`suites/${suiteHash}/stats.json`)
      if (!data) {
        throw new Error(`Failed to fetch suite stats: ${status}`)
      }
      return data
    },
    enabled: !!suiteHash,
  })
}
