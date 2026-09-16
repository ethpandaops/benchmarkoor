import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { SuiteStats } from '../types'
import { loadRuntimeConfig, isIndexingEnabled } from '@/config/runtime'
import { withoutBaselineTests } from '@/utils/baselineTests'

export function useSuiteStats(suiteHash: string | undefined) {
  return useQuery({
    queryKey: ['suiteStats', suiteHash],
    queryFn: async () => {
      const config = await loadRuntimeConfig()

      if (isIndexingEnabled(config) && config.api?.baseUrl) {
        const response = await fetch(
          `${config.api.baseUrl}/api/v1/index/suites/${suiteHash}/stats?max_runs_per_client=25`,
          { credentials: 'include' },
        )

        if (!response.ok) {
          throw new Error(`Failed to fetch suite stats: ${response.status}`)
        }

        // The API already excludes baseline rows by default; the client-side
        // filter stays for rollout skew against older API deployments.
        const stats = (await response.json()) as SuiteStats
        return withoutBaselineTests(stats)
      }

      const { data, status } = await fetchData<SuiteStats>(`suites/${suiteHash}/stats.json`)
      if (!data) {
        throw new Error(`Failed to fetch suite stats: ${status}`)
      }
      return withoutBaselineTests(data)
    },
    enabled: !!suiteHash,
  })
}
