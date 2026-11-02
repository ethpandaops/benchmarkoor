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

// Series colors, the same hues the resource usage charts use.
export const SERIES_COLORS = {
  pending: '#3b82f6',
  queued: '#f59e0b',
  memory: '#10b981',
  histogram: '#8b5cf6',
}
