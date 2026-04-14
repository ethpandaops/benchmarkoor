export interface LiveEta {
  /** Seconds of wall-clock time since the run started. Always present. */
  elapsedSec: number
  /**
   * Seconds estimated before the run completes, if computable. undefined
   * when we haven't seen any completed tests yet or have no total to
   * compare against.
   */
  remainingSec?: number
  /** Unix ms timestamp of the predicted completion, if computable. */
  etaAtMs?: number
}

/**
 * computeLiveEta produces a rolling-average estimate of remaining time
 * from the observed per-test rate. Returns undefined fields when we
 * can't compute (e.g. before the first test completes).
 */
export function computeLiveEta(startTimestampSec: number, completed: number, total: number): LiveEta {
  const nowMs = Date.now()
  const nowSec = Math.floor(nowMs / 1000)
  const elapsedSec = Math.max(0, nowSec - startTimestampSec)

  if (completed <= 0 || total <= 0 || completed >= total) {
    return { elapsedSec }
  }

  const remainingSec = Math.round((elapsedSec / completed) * (total - completed))
  return { elapsedSec, remainingSec, etaAtMs: nowMs + remainingSec * 1000 }
}

/**
 * Compact "~5m" label for placing next to progress counts.
 */
export function formatEtaShort(eta: LiveEta): string | undefined {
  if (eta.remainingSec === undefined) return undefined
  return `~${formatHoursMinutes(eta.remainingSec)} left`
}

/**
 * Multi-line tooltip with elapsed, remaining, and completion time.
 */
export function formatEtaTooltip(eta: LiveEta): string {
  const lines = [`Elapsed: ${formatHoursMinutes(eta.elapsedSec)}`]

  if (eta.remainingSec !== undefined) {
    lines.push(`~${formatHoursMinutes(eta.remainingSec)} remaining`)
  }

  if (eta.etaAtMs !== undefined) {
    lines.push(`ETA: ${formatClockHoursMinutes(new Date(eta.etaAtMs))}`)
  }

  if (eta.remainingSec === undefined && eta.elapsedSec >= 0) {
    lines.push('ETA: unknown (no tests completed yet)')
  }

  return lines.join('\n')
}

/**
 * formatHoursMinutes renders seconds as h/m, rounded to the nearest minute
 * (with a minimum display of "< 1m" when under 30s).
 */
export function formatHoursMinutes(totalSeconds: number): string {
  if (totalSeconds < 30) return '< 1m'

  const totalMinutes = Math.round(totalSeconds / 60)
  const hours = Math.floor(totalMinutes / 60)
  const minutes = totalMinutes % 60

  if (hours === 0) return `${minutes}m`
  if (minutes === 0) return `${hours}h`
  return `${hours}h ${minutes}m`
}

/**
 * formatClockHoursMinutes renders a Date as "HH:MM" in the browser's
 * current locale without seconds.
 */
export function formatClockHoursMinutes(d: Date): string {
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
}
