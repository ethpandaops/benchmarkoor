import { useEffect, useMemo } from 'react'

/** Where the open test sits in the list, and how to move from it. */
export interface TestNavigation {
  /** 1-based position of the open test in the list. */
  position: number
  total: number
  /** Opens the previous test; undefined on the first one. */
  onPrev?: () => void
  /** Opens the next test; undefined on the last one. */
  onNext?: () => void
}

/** True when a key press belongs to a field the user types in. */
function isTypingTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false
  const tag = target.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable
}

/**
 * Prev/next navigation for a test details modal. `order` is the list of
 * tests in the order the page shows them, `current` the open test, and
 * `select` opens another one. While a test is open, the Left and Right
 * arrow keys move through `order` (not inside a text field, and not with a
 * modifier key held). Returns undefined when `current` is not in `order`.
 */
export function useTestNavigation<K>(
  order: K[],
  current: K | null | undefined,
  select: (key: K) => void,
): TestNavigation | undefined {
  const index = current == null ? -1 : order.indexOf(current)

  const navigation = useMemo<TestNavigation | undefined>(() => {
    if (index < 0) return undefined
    return {
      position: index + 1,
      total: order.length,
      onPrev: index > 0 ? () => select(order[index - 1]) : undefined,
      onNext: index < order.length - 1 ? () => select(order[index + 1]) : undefined,
    }
  }, [order, index, select])

  useEffect(() => {
    if (!navigation) return
    const handleKey = (e: KeyboardEvent) => {
      if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey || e.shiftKey) return
      if (isTypingTarget(e.target)) return
      const go = e.key === 'ArrowLeft' ? navigation.onPrev : e.key === 'ArrowRight' ? navigation.onNext : undefined
      if (!go) return
      e.preventDefault()
      go()
    }
    document.addEventListener('keydown', handleKey)
    return () => document.removeEventListener('keydown', handleKey)
  }, [navigation])

  return navigation
}
