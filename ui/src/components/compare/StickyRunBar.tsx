import { useEffect, useRef, useState } from 'react'
import clsx from 'clsx'
import { type CompareRun, type LabelMode, buildLabelModeOptions, RUN_SLOTS, formatRunLabel } from './constants'
import { FilterInput } from '@/components/shared/FilterInput'

interface StickyRunBarProps {
  runs: CompareRun[]
  /** Ref to the element that, when scrolled out of view, triggers the sticky bar */
  sentinelRef: React.RefObject<HTMLDivElement | null>
  labelMode: LabelMode
  onLabelModeChange: (mode: LabelMode) => void
  testFilter: string
  testFilterRegex: boolean
  onTestFilterChange: (query: string) => void
  onTestFilterRegexChange: (enabled: boolean) => void
}

export function StickyRunBar({ runs, sentinelRef, labelMode, onLabelModeChange, testFilter, testFilterRegex, onTestFilterChange, onTestFilterRegexChange }: StickyRunBarProps) {
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
            <span key={slot.label} className={clsx('inline-flex items-center gap-1.5 rounded-sm px-2 py-0.5 text-xs/5 font-medium', slot.badgeBgClass, slot.badgeTextClass)}>
              <img src={`/img/clients/${run.config.instance.client}.jpg`} alt={run.config.instance.client} className="size-3.5 rounded-full object-cover" />
              {formatRunLabel(slot, run, labelMode)}
            </span>
          )
        })}
        <div className="flex items-center gap-1.5 text-xs/5 text-gray-500 dark:text-gray-400">
          <span>Labels:</span>
          <div className="flex gap-1">
            {buildLabelModeOptions(runs).map((opt) => (
              <button
                key={opt.value}
                onClick={() => onLabelModeChange(opt.value)}
                className={`rounded-xs px-2 py-0.5 text-xs/5 font-medium transition-colors ${
                  labelMode === opt.value
                    ? 'bg-gray-800 text-white dark:bg-gray-200 dark:text-gray-900'
                    : 'bg-gray-100 text-gray-500 hover:bg-gray-200 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600'
                }`}
              >
                {opt.label}
              </button>
            ))}
          </div>
        </div>
        <div className="flex items-center gap-1.5 text-xs/5 text-gray-500 dark:text-gray-400">
          <span>Filter:</span>
          <FilterInput
            placeholder={testFilterRegex ? 'Regex...' : 'Filter...'}
            value={testFilter}
            onValueChange={onTestFilterChange}
            className={clsx(
              'w-36 rounded-xs border bg-white px-2 py-0.5 text-xs/5 placeholder-gray-400 focus:outline-hidden focus:ring-1 dark:bg-gray-700 dark:text-gray-100 dark:placeholder-gray-500',
              testFilterRegex && testFilter && (() => { try { new RegExp(testFilter); return false } catch { return true } })()
                ? 'border-red-400 focus:border-red-500 focus:ring-red-500 dark:border-red-500'
                : 'border-gray-300 focus:border-blue-500 focus:ring-blue-500 dark:border-gray-600',
            )}
          />
          <button
            onClick={() => onTestFilterRegexChange(!testFilterRegex)}
            title={testFilterRegex ? 'Regex mode' : 'Text mode'}
            className={clsx(
              'rounded-xs px-1 py-0.5 font-mono text-xs/5 transition-colors',
              testFilterRegex
                ? 'bg-blue-500 text-white'
                : 'border border-gray-300 bg-white text-gray-500 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-400 dark:hover:bg-gray-600',
            )}
          >
            .*
          </button>
        </div>
      </div>
    </div>
  )
}
