import { type IndexEntry, type IndexStepType, getIndexAggregatedStats } from '@/api/types'

export type SortColumn = 'timestamp' | 'client' | 'image' | 'suite' | 'duration' | 'mgas' | 'failed' | 'passed' | 'total'
export type SortDirection = 'asc' | 'desc'

// Calculates MGas/s from gas_used and gas_used_duration
function calculateMGasPerSec(gasUsed: number, gasUsedDuration: number): number | undefined {
  if (gasUsedDuration <= 0 || gasUsed <= 0) return undefined
  return (gasUsed * 1000) / gasUsedDuration
}

export function sortIndexEntries(
  entries: IndexEntry[],
  sortBy: SortColumn,
  sortDir: SortDirection,
  stepFilter: IndexStepType[],
): IndexEntry[] {
  return [...entries].sort((a, b) => {
    let comparison = 0
    const statsA = getIndexAggregatedStats(a, stepFilter)
    const statsB = getIndexAggregatedStats(b, stepFilter)
    switch (sortBy) {
      case 'timestamp':
        comparison = a.timestamp - b.timestamp
        break
      case 'client':
        comparison = a.instance.client.localeCompare(b.instance.client)
        break
      case 'image':
        comparison = a.instance.image.localeCompare(b.instance.image)
        break
      case 'suite':
        comparison = (a.suite_hash ?? '').localeCompare(b.suite_hash ?? '')
        break
      case 'duration': {
        const durA = (a.timestamp_end ?? a.timestamp) - a.timestamp
        const durB = (b.timestamp_end ?? b.timestamp) - b.timestamp
        comparison = durA - durB
        break
      }
      case 'mgas': {
        const mgasA = calculateMGasPerSec(statsA.gasUsed, statsA.gasUsedDuration) ?? -Infinity
        const mgasB = calculateMGasPerSec(statsB.gasUsed, statsB.gasUsedDuration) ?? -Infinity
        comparison = mgasA - mgasB
        break
      }
      case 'failed':
        comparison = (a.tests.tests_total - a.tests.tests_passed) - (b.tests.tests_total - b.tests.tests_passed)
        break
      case 'passed':
        comparison = a.tests.tests_passed - b.tests.tests_passed
        break
      case 'total':
        comparison = a.tests.tests_total - b.tests.tests_total
        break
    }
    return sortDir === 'asc' ? comparison : -comparison
  })
}
