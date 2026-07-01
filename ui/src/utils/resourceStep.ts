import type { ResourceTotals } from '@/api/types'

// ResourceStep selects which benchmark step's resource usage the charts show:
// the combined total, or just the setup or test step in isolation.
export type ResourceStep = 'sum' | 'setup' | 'test'

export const DEFAULT_RESOURCE_STEP: ResourceStep = 'test'

// RESOURCE_STEP_OPTIONS drives the setup/test/sum toggle. "Sum" combines all
// steps (setup + test + cleanup) — the historical default for these charts.
export const RESOURCE_STEP_OPTIONS: { value: ResourceStep; label: string }[] = [
  { value: 'sum', label: 'Sum' },
  { value: 'setup', label: 'Setup' },
  { value: 'test', label: 'Test' },
]

type StepKey = 'setup' | 'test' | 'cleanup'

// resourceStepKeys maps a selection to the concrete step keys it aggregates.
// "sum" spans every step so the total matches what the charts showed before the
// toggle existed; "setup"/"test" isolate a single step.
export function resourceStepKeys(step: ResourceStep): StepKey[] {
  switch (step) {
    case 'setup':
      return ['setup']
    case 'test':
      return ['test']
    default:
      return ['setup', 'test', 'cleanup']
  }
}

// StepResource is the per-step resource input, normalised so both the per-test
// result shape (TestEntry, resource under `aggregated`) and the per-run index
// shape (IndexEntry, resource directly on the step) can feed the same helper.
export interface StepResource {
  resourceTotals?: ResourceTotals
  // Wall-clock time for the step in nanoseconds, used for CPU% (unused by the
  // index-shape charts, which don't render CPU%).
  timeTotalNs?: number
}

export interface AggregatedResource {
  totals: ResourceTotals
  timeTotalNs: number
  memoryBytes: number
}

// aggregateResourceByStep sums the resource totals of the steps selected by
// `step`. Cumulative metrics (cpu, disk, memory delta) are summed; absolute
// memory is taken as the max across steps (it's a snapshot, not cumulative);
// step wall-clock time is summed for CPU%. Returns undefined when none of the
// selected steps carry resource data.
export function aggregateResourceByStep(
  steps: Partial<Record<StepKey, StepResource | undefined>>,
  step: ResourceStep,
): AggregatedResource | undefined {
  let hasData = false
  let cpuUsec = 0
  let memoryDelta = 0
  let diskRead = 0
  let diskWrite = 0
  let diskReadIops = 0
  let diskWriteIops = 0
  let timeTotalNs = 0
  let memoryBytes = 0

  for (const key of resourceStepKeys(step)) {
    const entry = steps[key]
    if (!entry) continue

    timeTotalNs += entry.timeTotalNs ?? 0

    const res = entry.resourceTotals
    if (!res) continue

    hasData = true
    cpuUsec += res.cpu_usec ?? 0
    memoryDelta += res.memory_delta_bytes ?? 0
    diskRead += res.disk_read_bytes ?? 0
    diskWrite += res.disk_write_bytes ?? 0
    diskReadIops += res.disk_read_iops ?? 0
    diskWriteIops += res.disk_write_iops ?? 0

    const stepMemory = res.memory_bytes ?? 0
    if (stepMemory > memoryBytes) memoryBytes = stepMemory
  }

  if (!hasData) return undefined

  return {
    totals: {
      cpu_usec: cpuUsec,
      memory_delta_bytes: memoryDelta,
      memory_bytes: memoryBytes,
      disk_read_bytes: diskRead,
      disk_write_bytes: diskWrite,
      disk_read_iops: diskReadIops,
      disk_write_iops: diskWriteIops,
    },
    timeTotalNs,
    memoryBytes,
  }
}
