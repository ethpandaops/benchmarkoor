import { useEffect, useRef, useState } from 'react'
import clsx from 'clsx'
import { type CompareRun, type LabelMode, RUN_SLOTS, formatRunLabel } from './constants'

interface StickyRunBarProps {
  runs: CompareRun[]
  /** Ref to the element that, when scrolled out of view, triggers the sticky bar */
  sentinelRef: React.RefObject<HTMLDivElement | null>
  labelMode: LabelMode
}

export function StickyRunBar({ runs, sentinelRef, labelMode }: StickyRunBarProps) {
  const [visible, setVisible] = useState(false)
  const observerRef = useRef<IntersectionObserver | null>(null)

  useEffect(() => {
    const el = sentinelRef.current
    if (!el) return

    observerRef.current = new IntersectionObserver(
      ([entry]) => setVisible(!entry.isIntersecting),
      { threshold: 0 },
    )
    observerRef.current.observe(el)

    return () => observerRef.current?.disconnect()
  }, [sentinelRef])

  if (!visible) return null

  return (
    <div className="fixed top-0 right-0 left-0 z-50 border-b border-gray-200 bg-white/95 backdrop-blur-sm dark:border-gray-700 dark:bg-gray-900/95">
      <div className="mx-auto flex max-w-7xl items-center justify-center gap-4 px-4 py-2">
        {runs.map((run) => {
          const slot = RUN_SLOTS[run.index]
          return (
            <div key={slot.label} className="flex items-center gap-2">
              <span className={clsx('inline-flex items-center gap-1.5 rounded-sm px-2 py-0.5 text-xs/5 font-medium', slot.badgeBgClass, slot.badgeTextClass)}>
                <img src={`/img/clients/${run.config.instance.client}.jpg`} alt={run.config.instance.client} className="size-3.5 rounded-full object-cover" />
                {formatRunLabel(slot, run, labelMode)}
              </span>
              {run.config.instance.id && (
                <span className="truncate font-mono text-xs/5 text-gray-500 dark:text-gray-400" title={run.config.instance.id}>
                  {run.config.instance.id}
                </span>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}
