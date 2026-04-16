import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { ChevronDown, ScrollText, AlertTriangle } from 'lucide-react'
import clsx from 'clsx'
import { useLiveRunLogsWS } from '@/api/hooks/useLiveRunLogsWS'

interface LiveRunLogPanelProps {
  runId: string
}

/**
 * LiveRunLogPanel streams the runner's benchmarkoor.log over a
 * WebSocket while the user has it expanded. Collapsed = no WS open =
 * no log traffic from the runner. The hook does all the heavy lifting;
 * this component handles layout, sticky-bottom auto-scroll, and the
 * expand/collapse toggle.
 */
export function LiveRunLogPanel({ runId }: LiveRunLogPanelProps) {
  const [expanded, setExpanded] = useState(false)
  const { text, truncated, ended, connected } = useLiveRunLogsWS(runId, expanded)

  // Sticky-bottom auto-scroll: only snap on new data when the user is
  // already at the bottom, so a user scrolling up to read doesn't get
  // yanked back.
  const scrollRef = useRef<HTMLPreElement>(null)
  const wasAtBottomRef = useRef(true)

  const handleScroll = () => {
    const el = scrollRef.current
    if (!el) return
    wasAtBottomRef.current = el.scrollTop + el.clientHeight >= el.scrollHeight - 8
  }

  useLayoutEffect(() => {
    const el = scrollRef.current
    if (!el) return
    if (wasAtBottomRef.current) {
      el.scrollTop = el.scrollHeight
    }
  }, [text])

  // When (re)expanding, reset the "was at bottom" flag so the first
  // snapshot lands at the bottom even if the prior session had scrolled.
  useEffect(() => {
    if (expanded) {
      wasAtBottomRef.current = true
    }
  }, [expanded])

  return (
    <div className="overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        className={clsx(
          'flex w-full cursor-pointer items-center justify-between gap-3 px-4 py-3 text-left',
          expanded && 'border-b border-gray-200 dark:border-gray-700',
          'hover:bg-gray-50 dark:hover:bg-gray-700/50',
        )}
      >
        <h3 className="flex items-center gap-2 text-sm/6 font-medium text-gray-900 dark:text-gray-100">
          <ScrollText className="size-4 text-gray-400 dark:text-gray-500" />
          Runner log (live)
          {expanded && (
            <span
              className={clsx(
                'ml-1 inline-flex items-center gap-1 rounded-xs px-1.5 py-0.5 text-xs font-medium',
                connected
                  ? 'bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-200'
                  : 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300',
              )}
            >
              <span
                className={clsx(
                  'size-1.5 rounded-full',
                  connected ? 'bg-green-500' : 'bg-gray-400',
                )}
                aria-hidden="true"
              />
              {connected ? 'connected' : 'connecting…'}
            </span>
          )}
        </h3>
        <ChevronDown
          className={clsx(
            'size-5 shrink-0 text-gray-500 transition-transform',
            expanded && 'rotate-180',
          )}
        />
      </button>

      {expanded && (
        <div className="flex flex-col">
          {truncated && (
            <div className="flex items-center gap-2 border-b border-yellow-200 bg-yellow-50 px-4 py-2 text-xs/5 text-yellow-800 dark:border-yellow-900/50 dark:bg-yellow-900/20 dark:text-yellow-200">
              <AlertTriangle className="size-3.5 shrink-0" />
              Older log lines were dropped to stay under the server buffer cap.
            </div>
          )}
          {ended && (
            <div className="border-b border-gray-200 bg-gray-50 px-4 py-2 text-xs/5 text-gray-600 dark:border-gray-700 dark:bg-gray-900/40 dark:text-gray-400">
              Run ended. Log stream closed.
            </div>
          )}
          <pre
            ref={scrollRef}
            onScroll={handleScroll}
            className="h-64 max-h-[50vh] overflow-auto whitespace-pre-wrap bg-gray-950 px-4 py-3 font-mono text-xs/5 text-gray-100"
          >
            {text.length === 0 ? (
              <span className="text-gray-500">
                {connected ? 'Waiting for log output…' : 'Connecting…'}
              </span>
            ) : (
              text
            )}
          </pre>
        </div>
      )}
    </div>
  )
}
