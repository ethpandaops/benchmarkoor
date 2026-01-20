import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { SuiteStats } from '../types'

export function useSuiteStats(suiteHash: string | undefined) {
  return useQuery({
    queryKey: ['suiteStats', suiteHash],
    queryFn: () => fetchData<SuiteStats>(`suites/${suiteHash}/stats.json`),
    enabled: !!suiteHash,
  })
}
