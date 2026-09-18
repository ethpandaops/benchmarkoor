import { useSyncExternalStore } from 'react'

export type LayoutWidth = 'contained' | 'wide'

// index.html reads the same param and key on load and adds the `wide`
// class before the first paint, so the page does not jump from narrow to
// wide.
const STORAGE_KEY = 'benchmarkoor:layout-width'
const URL_PARAM = 'layout'
const WIDE_CLASS = 'wide'
const DEFAULT_WIDTH: LayoutWidth = 'contained'

function parseWidth(v: string | null): LayoutWidth | undefined {
  return v === 'wide' || v === 'contained' ? v : undefined
}

function readFromUrl(): LayoutWidth | undefined {
  if (typeof window === 'undefined') return undefined
  return parseWidth(new URLSearchParams(window.location.search).get(URL_PARAM))
}

function readFromStorage(): LayoutWidth | undefined {
  if (typeof window === 'undefined') return undefined
  return parseWidth(localStorage.getItem(STORAGE_KEY))
}

// URL beats localStorage so a shared link is honored even if the recipient
// has a different preference saved.
let currentWidth: LayoutWidth = readFromUrl() ?? readFromStorage() ?? DEFAULT_WIDTH

const listeners = new Set<() => void>()

function subscribe(listener: () => void): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

function getSnapshot(): LayoutWidth {
  return currentWidth
}

function getServerSnapshot(): LayoutWidth {
  return DEFAULT_WIDTH
}

export function setLayoutWidth(next: LayoutWidth): void {
  if (next === currentWidth) return
  currentWidth = next
  if (typeof window !== 'undefined') {
    localStorage.setItem(STORAGE_KEY, next)
    // The `wide:` Tailwind variant keys off this class (see index.css).
    document.documentElement.classList.toggle(WIDE_CLASS, next === 'wide')
    const url = new URL(window.location.href)
    // Keep URLs clean at the default; only persist `wide` explicitly.
    if (next === DEFAULT_WIDTH) url.searchParams.delete(URL_PARAM)
    else url.searchParams.set(URL_PARAM, next)
    window.history.replaceState(null, '', url)
  }
  listeners.forEach((l) => l())
}

/** The page width preference: contained (max-w-7xl) or the full viewport. */
export function useLayoutWidth(): {
  width: LayoutWidth
  setWidth: (width: LayoutWidth) => void
  toggle: () => void
} {
  const width = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot)
  return {
    width,
    setWidth: setLayoutWidth,
    toggle: () => setLayoutWidth(width === 'wide' ? 'contained' : 'wide'),
  }
}
