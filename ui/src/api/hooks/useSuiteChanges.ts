import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { IndexStepType, SuiteChanges } from '../types'
import { fetchSuiteStats, suiteStatsQueryKey } from './useSuiteStats'
import { loadRuntimeConfig, isIndexingEnabled } from '@/config/runtime'
import { buildSuiteChanges } from '@/utils/regressions'

// Returns the latest step of each client and test in its last `stepWindow` runs.
// With indexing, the API builds this. Without it, the UI builds it from the
// suite's stats.json.
export function useSuiteChanges(suiteHash: string | undefined, stepFilter: IndexStepType[], stepWindow: number) {
  const queryClient = useQueryClient()
  const steps = [...stepFilter].sort().join(',')

  return useQuery({
    queryKey: ['suiteChanges', suiteHash, steps, stepWindow],
    queryFn: async (): Promise<SuiteChanges> => {
      const config = await loadRuntimeConfig()

      if (isIndexingEnabled(config) && config.api?.baseUrl) {
        const response = await fetch(
          `${config.api.baseUrl}/api/v1/index/suites/${suiteHash}/changes?${new URLSearchParams({ steps, window: String(stepWindow) })}`,
          { credentials: 'include' },
        )

        if (!response.ok) {
          throw new Error(`Failed to fetch suite changes: ${response.status}`)
        }

        return response.json() as Promise<SuiteChanges>
      }

      // Share the stats.json download with the Tests tab.
      const stats = await queryClient.ensureQueryData({
        queryKey: suiteStatsQueryKey(suiteHash),
        queryFn: () => fetchSuiteStats(suiteHash),
      })
      return buildSuiteChanges(stats, stepFilter, stepWindow)
    },
    enabled: !!suiteHash,
  })
}
