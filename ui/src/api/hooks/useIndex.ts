import { useQuery } from '@tanstack/react-query'
import { fetchData, fetchViaS3 } from '../client'
import type { Index } from '../types'
import { loadRuntimeConfig, isS3Mode, registerDiscoveryMapping } from '@/config/runtime'

const emptyIndex: Index = { generated: 0, entries: [] }

async function fetchS3Index(): Promise<Index> {
  const config = await loadRuntimeConfig()
  const paths = config.storage?.s3?.discovery_paths ?? []

  if (paths.length === 0) return emptyIndex

  const results = await Promise.all(
    paths.map(async (dp) => {
      try {
        const url = `${config.api!.baseUrl}/api/v1/files/${dp}/index.json`
        const response = await fetchViaS3(url)
        if (!response.ok) return null
        const contentType = response.headers.get('content-type')
        if (!contentType?.includes('application/json')) return null
        const index: Index = await response.json()
        // Register discovery mappings for each entry
        for (const entry of index.entries) {
          registerDiscoveryMapping(entry.run_id, dp)
          if (entry.suite_hash) {
            registerDiscoveryMapping(entry.suite_hash, dp)
          }
        }
        return index
      } catch {
        return null
      }
    }),
  )

  // Merge all index entries
  const allEntries = results
    .filter((r): r is Index => r !== null)
    .flatMap((r) => r.entries)

  if (allEntries.length === 0) return emptyIndex

  // Use the most recent generated timestamp
  const generated = Math.max(
    ...results.filter((r): r is Index => r !== null).map((r) => r.generated),
  )

  return { generated, entries: allEntries }
}

export function useIndex() {
  return useQuery({
    queryKey: ['index'],
    queryFn: async () => {
      const config = await loadRuntimeConfig()

      if (isS3Mode(config)) {
        return fetchS3Index()
      }

      const { data, status } = await fetchData<Index>('runs/index.json')

      if (status === 404) {
        return emptyIndex
      }

      if (!data) {
        throw new Error(`Failed to fetch index: ${status}`)
      }

      return data
    },
  })
}
