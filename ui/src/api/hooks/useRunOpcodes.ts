import { useQuery } from '@tanstack/react-query'
import { fetchData } from '../client'
import type { RunTestOpcodes } from '../types'

/**
 * useRunOpcodes fetches a run's test-opcodes.json (written when the
 * runner had `opcode_extraction.enabled = true`). Returns null when
 * the file is absent, which is the common case (extraction is opt-in).
 */
export function useRunOpcodes(runId: string, enabled = true) {
  return useQuery({
    queryKey: ['run', runId, 'opcodes'],
    queryFn: async () => {
      const { data } = await fetchData<RunTestOpcodes>(`runs/${runId}/test-opcodes.json`)
      return data ?? null
    },
    enabled: !!runId && enabled,
  })
}
