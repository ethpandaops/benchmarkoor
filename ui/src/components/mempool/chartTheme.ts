import { useEffect, useState } from 'react'

// useIsDark follows the "dark" class on the document root, as the other
// chart components do.
export function useIsDark(): boolean {
  const [isDark, setIsDark] = useState(() => document.documentElement.classList.contains('dark'))

  useEffect(() => {
    const observer = new MutationObserver(() => {
      setIsDark(document.documentElement.classList.contains('dark'))
    })
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ['class'] })
    return () => observer.disconnect()
  }, [])

  return isDark
}

export interface ChartTheme {
  text: string
  muted: string
  axisLine: string
  splitLine: string
  tooltipBg: string
  tooltipBorder: string
}

export function chartTheme(isDark: boolean): ChartTheme {
  return {
    text: isDark ? '#e5e7eb' : '#374151',
    muted: isDark ? '#9ca3af' : '#6b7280',
    axisLine: isDark ? '#4b5563' : '#d1d5db',
    splitLine: isDark ? '#374151' : '#e5e7eb',
    tooltipBg: isDark ? '#1f2937' : '#ffffff',
    tooltipBorder: isDark ? '#374151' : '#e5e7eb',
  }
}

// Series colors in a fixed order: pending, queued. Queued uses amber-600,
// which passes the palette validator on both the light and dark surface.
export const SERIES_COLORS = {
  pending: '#3b82f6',
  queued: '#d97706',
  memory: '#10b981',
  histogram: '#9ca3af',
}

// Event kind colors (light, dark). Each kind has its own lane, so the color
// only helps; the lane label carries the identity. Engine API calls share a
// slate family so the scenario's own events stand out.
const EVENT_COLORS: Record<string, [string, string]> = {
  send: ['#1baf7a', '#199e70'],
  pending: ['#6250d6', '#9085e9'],
  settled: ['#008300', '#22a322'],
  reorg: ['#e87ba4', '#d55181'],
  basefee: ['#eb6834', '#d95926'],
  fcu: ['#64748b', '#94a3b8'],
  newPayload: ['#475569', '#cbd5e1'],
  getPayload: ['#94a3b8', '#64748b'],
}

const EVENT_FALLBACK: [string, string][] = [
  ['#eda100', '#c98500'],
  ['#e34948', '#e66767'],
  ['#2a78d6', '#3987e5'],
]

export function eventKindColor(kind: string, index: number, isDark: boolean): string {
  const pair = EVENT_COLORS[kind] ?? EVENT_FALLBACK[index % EVENT_FALLBACK.length]
  return isDark ? pair[1] : pair[0]
}
