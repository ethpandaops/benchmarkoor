import { formatDurationMs } from '@/utils/format'

// How an indexing pass trigger reads in the UI. The API stores the bare verb;
// "scheduled" is the only one that needs rewording.
export const PASS_TRIGGER_LABELS: Record<string, string> = {
  startup: 'startup',
  schedule: 'scheduled',
  manual: 'manual',
}

export function passTriggerLabel(trigger: string): string {
  return PASS_TRIGGER_LABELS[trigger] ?? trigger
}

/**
 * How long a pass took. The store keeps whole milliseconds, so a pass on a
 * small deployment can round to zero: say "under a millisecond" rather than
 * the shared formatter's "0ns", which claims a precision the row never had.
 */
export function formatPassDuration(milliseconds: number): string {
  return milliseconds === 0 ? '<1ms' : formatDurationMs(milliseconds)
}
