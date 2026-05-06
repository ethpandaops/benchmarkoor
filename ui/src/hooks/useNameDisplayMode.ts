import { useSyncExternalStore } from 'react'
import { formatTestName } from '@/utils/eestName'

export type NameDisplayMode = 'decomposed' | 'raw'

const STORAGE_KEY = 'benchmarkoor:name-display'
const URL_PARAM = 'names'
const DEFAULT_MODE: NameDisplayMode = 'decomposed'

function readFromUrl(): NameDisplayMode | undefined {
  if (typeof window === 'undefined') return undefined
  const v = new URLSearchParams(window.location.search).get(URL_PARAM)
  return v === 'raw' || v === 'decomposed' ? v : undefined
}

function readFromStorage(): NameDisplayMode | undefined {
  if (typeof window === 'undefined') return undefined
  const v = localStorage.getItem(STORAGE_KEY)
  return v === 'raw' || v === 'decomposed' ? v : undefined
}

// URL beats localStorage so a shared link is honored even if the recipient
// has a different preference saved.
let currentMode: NameDisplayMode = (() => {
  if (typeof window === 'undefined') return DEFAULT_MODE
  return readFromUrl() ?? readFromStorage() ?? DEFAULT_MODE
})()

const listeners = new Set<() => void>()

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

function getSnapshot(): NameDisplayMode {
  return currentMode
}

function getServerSnapshot(): NameDisplayMode {
  return DEFAULT_MODE
}

export function setNameDisplayMode(next: NameDisplayMode): void {
  if (next === currentMode) return
  currentMode = next
  if (typeof window !== 'undefined') {
    localStorage.setItem(STORAGE_KEY, next)
    const url = new URL(window.location.href)
    // Keep URLs clean when at the default; only persist `raw` explicitly.
    if (next === DEFAULT_MODE) url.searchParams.delete(URL_PARAM)
    else url.searchParams.set(URL_PARAM, next)
    window.history.replaceState(null, '', url)
  }
  listeners.forEach((l) => l())
}

export function useNameDisplayMode(): {
  mode: NameDisplayMode
  setMode: (mode: NameDisplayMode) => void
  toggle: () => void
} {
  const mode = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)
  return {
    mode,
    setMode: setNameDisplayMode,
    toggle: () => setNameDisplayMode(mode === 'decomposed' ? 'raw' : 'decomposed'),
  }
}

/**
 * Returns the test name formatted for plain-text contexts (heatmap row
 * labels, hover tooltips), respecting the global display mode.
 */
export function useFormattedTestName(name: string): string {
  const mode = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)
  return formatTestName(name, mode)
}
