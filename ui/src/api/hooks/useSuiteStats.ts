import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { SuiteStats } from '../types'
import { loadRuntimeConfig, isIndexingEnabled } from '@/config/runtime'

export const suiteStatsQueryKey = (suiteHash: string | undefined) => ['suiteStats', suiteHash]

export async function fetchSuiteStats(suiteHash: string | undefined): Promise<SuiteStats> {
  const config = await loadRuntimeConfig()

  if (isIndexingEnabled(config) && config.api?.baseUrl) {
    const response = await fetch(
      `${config.api.baseUrl}/api/v1/index/suites/${suiteHash}/stats?max_runs_per_client=25`,
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
}

// The stats of every test in the recent runs. For a suite with thousands of
// tests this is a large download, so load it only where it is shown.
export function useSuiteStats(suiteHash: string | undefined, enabled = true) {
  return useQuery({
    queryKey: suiteStatsQueryKey(suiteHash),
    queryFn: () => fetchSuiteStats(suiteHash),
    enabled: !!suiteHash && enabled,
  })
}
