import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import {
  ArrowDown,
  ScrollText,
  AlertTriangle,
  Maximize2,
  Plug,
  Unplug,
  X,
} from 'lucide-react'
import clsx from 'clsx'
import { useLiveRunLogsWS } from '@/api/hooks/useLiveRunLogsWS'
import { AnsiLine } from '@/components/shared/AnsiLine'

interface LiveRunLogPanelProps {
  runId: string
  client?: string
  instanceId?: string
}

// After INACTIVITY_GRACE_MS of no user interaction, a countdown
// appears. If the user stays idle for another AUTO_STOP_COUNTDOWN_MS,
// the stream disconnects. Any interaction resets both timers.
const INACTIVITY_GRACE_MS = 30 * 1000
const AUTO_STOP_COUNTDOWN_MS = 5 * 60 * 1000

// Estimated line height for the virtualizer. Doesn't need to be exact;
// the virtualizer measures actual rendered heights dynamically.
const LINE_HEIGHT_ESTIMATE = 20

/**
 * LiveRunLogPanel streams the runner's benchmarkoor.log over a
 * WebSocket and renders each line with ANSI coloring. Uses row
 * virtualization so only visible lines are in the DOM — the total line
 * count can grow unbounded without causing render jank.
 */
type LogFilter = 'all' | 'runner' | 'client'

// Emoji prefixes used by the runner's logrus formatter to distinguish
// benchmarkoor's own log lines from lines forwarded from the EL client.
const RUNNER_PREFIX = '\u{1F535}' // 🔵
const CLIENT_PREFIX = '\u{1F7E3}' // 🟣

function lineMatchesFilter(line: string, filter: LogFilter): boolean {
  if (filter === 'all') return true
  if (filter === 'runner') return line.startsWith(RUNNER_PREFIX)
  return line.startsWith(CLIENT_PREFIX) // 'client'
}

export function LiveRunLogPanel({ runId, client, instanceId }: LiveRunLogPanelProps) {
  const [fullscreen, setFullscreen] = useState(false)
  const [stopped, setStopped] = useState(false)
  const [logFilter, setLogFilter] = useState<LogFilter>('all')
  const [searchQuery, setSearchQuery] = useState('')

  // ─── Inactivity-based auto-stop ────────────────────────────────
  // Phase 1 (grace): user is active — no countdown shown.
  // Phase 2 (countdown): 30 s of inactivity passed — countdown
  //   badge appears, ticking down from 5 min. If the user interacts
  //   at any point, we go back to Phase 1.
  // Phase 3 (stop): countdown reaches 0 → stream disconnects.
  //
  // autoStopAt is null during Phase 1 (no countdown), or a
  // wall-clock timestamp for the disconnect deadline (Phase 2).
  const [autoStopAt, setAutoStopAt] = useState<number | null>(null)
  const [now, setNow] = useState(() => Date.now())
  const lastActivityRef = useRef(Date.now())

  // Listen to page-wide interaction events while the stream is open.
  useEffect(() => {
    if (stopped) return

    const handler = () => { lastActivityRef.current = Date.now() }
    const events = ['mousemove', 'mousedown', 'keydown', 'scroll', 'touchstart'] as const
    for (const e of events) window.addEventListener(e, handler, { passive: true })
    return () => { for (const e of events) window.removeEventListener(e, handler) }
  }, [stopped])

  // Check inactivity every second; transition between phases.
  useEffect(() => {
    if (stopped) return

    const id = window.setInterval(() => {
      const idleMs = Date.now() - lastActivityRef.current
      setNow(Date.now())

      if (autoStopAt !== null) {
        // Phase 2 — check if user became active again.
        if (idleMs < INACTIVITY_GRACE_MS) {
          setAutoStopAt(null) // back to Phase 1
        } else if (Date.now() >= autoStopAt) {
          setStopped(true) // Phase 3
        }
      } else {
        // Phase 1 — check if we should start the countdown.
        if (idleMs >= INACTIVITY_GRACE_MS) {
          setAutoStopAt(Date.now() + AUTO_STOP_COUNTDOWN_MS) // enter Phase 2
        }
      }
    }, 1000)

    return () => window.clearInterval(id)
  }, [stopped, autoStopAt])

  const { textRef, version, truncated, ended, connected } = useLiveRunLogsWS(runId, !stopped)

  // ─── Incremental line tracking ─────────────────────────────────
  // Instead of re-splitting the entire text on every WS message, we
  // only process the delta. `linesRef` is a stable array mutated in
  // place; `lineVersion` is bumped to tell the virtualizer the count
  // changed. This makes each update O(new lines) instead of O(all).
  const linesRef = useRef<string[]>([])
  const prevTextLenRef = useRef(0)
  // Tracks whether the previous chunk ended with '\n'. When true,
  // the first segment of the next chunk is a NEW line (not a
  // continuation of the previous one). This was the source of the
  // multi-entry-on-one-line bug — without this flag, every first
  // segment got merged with the previous complete line.
  const prevEndedWithNewlineRef = useRef(true)
  const [lineVersion, setLineVersion] = useState(0)

  useEffect(() => {
    const dt = textRef.current
    const prevLen = prevTextLenRef.current

    if (dt.length < prevLen || (prevLen === 0 && dt.length > 0)) {
      // Full reset — text was replaced (snapshot on reconnect, or
      // paused→resumed with different content). Re-split entirely.
      const parts = dt.split('\n')
      if (parts.length > 0 && parts[parts.length - 1] === '') parts.pop()
      linesRef.current = parts
      prevEndedWithNewlineRef.current = dt.endsWith('\n')
    } else if (dt.length > prevLen) {
      // Incremental append — only process the new bytes.
      const delta = dt.slice(prevLen)
      const parts = delta.split('\n')

      if (parts.length > 0) {
        if (linesRef.current.length > 0 && !prevEndedWithNewlineRef.current) {
          // Previous chunk ended mid-line → first segment is a
          // continuation of that partial line.
          linesRef.current[linesRef.current.length - 1] += parts[0]
          parts.shift()
        }

        // Remaining segments are complete new lines.
        for (const p of parts) linesRef.current.push(p)

        // Trim trailing empty (from a final '\n' in the delta).
        if (linesRef.current.length > 0 && linesRef.current[linesRef.current.length - 1] === '') {
          linesRef.current.pop()
        }
      }

      prevEndedWithNewlineRef.current = delta.endsWith('\n')
    }

    prevTextLenRef.current = dt.length
     
    setLineVersion((v) => v + 1)
  }, [version]) // eslint-disable-line react-hooks/exhaustive-deps

  // ─── Filtered line indices ──────────────────────────────────────
  // Computed synchronously during render (useMemo, not useEffect) so
  // the virtualizer sees the correct count on the SAME frame the
  // filter changes — no one-frame flicker of the unfiltered list.
  const needsFiltering = logFilter !== 'all' || searchQuery !== ''
  const searchLower = searchQuery.toLowerCase()

  const { filteredIndices, filteredCount } = useMemo(() => {
    if (!needsFiltering) {
      return { filteredIndices: [] as number[], filteredCount: linesRef.current.length }
    }

    const indices: number[] = []
    for (let i = 0; i < linesRef.current.length; i++) {
      const line = linesRef.current[i]
      if (!lineMatchesFilter(line, logFilter)) continue
      if (searchLower && !line.toLowerCase().includes(searchLower)) continue
      indices.push(i)
    }

    return { filteredIndices: indices, filteredCount: indices.length }
    // lineVersion is a proxy for "linesRef.current changed"
  }, [lineVersion, logFilter, needsFiltering, searchLower]) // eslint-disable-line react-hooks/exhaustive-deps

  // Helper: map a virtualizer row to the actual line in linesRef.
  const lineAt = useCallback(
    (virtualRow: number): string => {
      if (!needsFiltering) {
        return linesRef.current[virtualRow] ?? ''
      }

      return linesRef.current[filteredIndices[virtualRow]] ?? ''
    },
    [needsFiltering, filteredIndices],
  )

  // ─── Virtualizer ────────────────────────────────────────────────
  const scrollRef = useRef<HTMLDivElement>(null)

  const handleWheel = useCallback(
    (e: React.WheelEvent) => {
      if (e.deltaY < 0 && !stopped) {
        setStopped(true)
        setAutoStopAt(null)
      }
    },
    [stopped],
  )

  const virtualizer = useVirtualizer({
    count: filteredCount,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => LINE_HEIGHT_ESTIMATE,
    overscan: 30,
  })

  useLayoutEffect(() => {
    if (stopped) return
    const count = filteredCount
    if (count > 0) {
      virtualizer.scrollToIndex(count - 1, { align: 'end' })
    }
  }, [lineVersion, stopped]) // eslint-disable-line react-hooks/exhaustive-deps

  const jumpToBottom = useCallback(() => {
    setStopped(false)
    setAutoStopAt(null)
    lastActivityRef.current = Date.now()
    const count = filteredCount
    if (count > 0) {
      virtualizer.scrollToIndex(count - 1, { align: 'end' })
    }
  }, [virtualizer, filteredCount])

  // ─── Fullscreen ────────────────────────────────────────────────
  useEffect(() => {
    if (!fullscreen) return
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setFullscreen(false)
    }
    document.addEventListener('keydown', handleKey)
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', handleKey)
      document.body.style.overflow = ''
    }
  }, [fullscreen])



  // ─── Header ────────────────────────────────────────────────────
  const statusBadge = (() => {
    if (stopped) {
      return {
        text: 'stopped',
        dot: 'bg-gray-400',
        wrap: 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300',
      }
    }
    if (connected) {
      return {
        text: 'connected',
        dot: 'bg-green-500',
        wrap: 'bg-green-100 text-green-800 dark:bg-green-900/40 dark:text-green-200',
      }
    }
    return {
      text: 'connecting…',
      dot: 'bg-gray-400',
      wrap: 'bg-gray-100 text-gray-700 dark:bg-gray-700 dark:text-gray-300',
    }
  })()

  const header = (
    <div
      className={clsx(
        'flex items-center justify-between gap-3 border-b border-gray-200 px-4 py-3',
        'dark:border-gray-700',
      )}
    >
      <h3 className="flex min-w-0 items-center gap-2 text-sm/6 font-medium text-gray-900 dark:text-gray-100">
        <ScrollText className="size-4 shrink-0 text-gray-400 dark:text-gray-500" />
        Runner log (live)
        {fullscreen && client && (
          <>
            <span className="text-gray-300 dark:text-gray-600">·</span>
            <img
              src={`/img/clients/${client}.jpg`}
              alt={`${client} logo`}
              className="size-5 shrink-0 rounded-xs object-cover"
            />
            {instanceId && (
              <span className="truncate font-mono text-gray-700 dark:text-gray-200">
                {instanceId}
              </span>
            )}
          </>
        )}
        {fullscreen && (
          <>
            <span className="text-gray-300 dark:text-gray-600">·</span>
            <span className="truncate font-mono text-xs text-gray-500 dark:text-gray-400">
              {runId}
            </span>
          </>
        )}
        <span
          className={clsx(
            'ml-1 inline-flex items-center gap-1 rounded-xs px-1.5 py-0.5 text-xs font-medium',
            statusBadge.wrap,
          )}
        >
          <span className={clsx('size-1.5 rounded-full', statusBadge.dot)} aria-hidden="true" />
          {statusBadge.text}
        </span>
        {!stopped && autoStopAt !== null && (() => {
          const remainingMs = Math.max(0, autoStopAt - now)
          const totalSec = Math.ceil(remainingMs / 1000)
          const mm = Math.floor(totalSec / 60)
          const ss = totalSec % 60
          const label = mm > 0 ? `${mm}m ${ss.toString().padStart(2, '0')}s` : `${ss}s`
          return (
            <span
              className="inline-flex items-center rounded-xs bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-800 dark:bg-amber-900/40 dark:text-amber-200"
              title="Move your mouse or press a key to cancel the countdown."
            >
              Stopping in {label} due to inactivity
            </span>
          )
        })()}
      </h3>
      <div className="flex shrink-0 items-center gap-2">
        {/* Search box */}
        <input
          type="text"
          value={searchQuery}
          onChange={(e) => setSearchQuery(e.target.value)}
          placeholder="Filter logs…"
          className="w-36 rounded-xs border border-gray-300 bg-white px-2 py-1 text-xs/5 placeholder-gray-400 focus:border-blue-500 focus:outline-hidden focus:ring-1 focus:ring-blue-500 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-100 dark:placeholder-gray-500"
        />
        {/* Log source filter chips */}
        <div className="flex items-center rounded-xs border border-gray-300 dark:border-gray-600">
          {([
            { value: 'all' as LogFilter, label: 'All' },
            { value: 'runner' as LogFilter, label: '🔵 Runner' },
            { value: 'client' as LogFilter, label: '🟣 Client' },
          ]).map(({ value, label }) => (
            <button
              key={value}
              type="button"
              onClick={() => setLogFilter(value)}
              className={clsx(
                'px-2 py-1 text-xs/5 font-medium transition-colors',
                'first:rounded-l-xs last:rounded-r-xs',
                logFilter === value
                  ? 'bg-blue-100 text-blue-800 dark:bg-blue-900/40 dark:text-blue-200'
                  : 'bg-white text-gray-600 hover:bg-gray-50 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600',
              )}
            >
              {label}
            </button>
          ))}
        </div>
        <button
          type="button"
          onClick={() => {
            if (stopped) {
              setStopped(false)
              setAutoStopAt(null)
              lastActivityRef.current = Date.now()
            } else {
              setStopped(true)
              setAutoStopAt(null)
            }
          }}
          className="rounded-xs border border-gray-300 bg-white px-2 py-1 text-sm/6 text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600"
          title={
            stopped
              ? 'Reconnect to log stream'
              : autoStopAt
                ? `Stop log stream (auto-stops at ${new Date(autoStopAt).toLocaleTimeString()})`
                : 'Stop log stream (disconnect)'
          }
          aria-label={stopped ? 'Reconnect log stream' : 'Stop log stream'}
        >
          {stopped ? <Plug className="size-4" /> : <Unplug className="size-4" />}
        </button>
        <button
          type="button"
          onClick={() => setFullscreen((v) => !v)}
          className="rounded-xs border border-gray-300 bg-white px-2 py-1 text-sm/6 text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600"
          title={fullscreen ? 'Exit fullscreen' : 'Fullscreen'}
          aria-label={fullscreen ? 'Exit fullscreen' : 'Fullscreen'}
        >
          {fullscreen ? <X className="size-4" /> : <Maximize2 className="size-4" />}
        </button>
      </div>
    </div>
  )

  // ─── Banners ───────────────────────────────────────────────────
  const banners = (
    <>
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
    </>
  )

  // ─── Virtualized log viewport ──────────────────────────────────
  const lines = linesRef.current
  const logViewport = (
    <div className={clsx('relative', fullscreen ? 'min-h-0 flex-1' : '')}>
      <div
        ref={scrollRef}
        onWheel={handleWheel}
        className={clsx(
          'overflow-auto bg-gray-950 font-mono text-xs/5 text-gray-100',
          fullscreen ? 'h-full' : 'h-96 max-h-[60vh]',
        )}
      >
        {lines.length === 0 ? (
          <div className="px-4 py-3 text-gray-500">
            {stopped
              ? 'Stream stopped. Click the reconnect button to resume.'
              : connected
                ? 'Waiting for log output…'
                : 'Connecting…'}
          </div>
        ) : (
          <div
            style={{
              height: virtualizer.getTotalSize(),
              width: '100%',
              position: 'relative',
            }}
          >
            {virtualizer.getVirtualItems().map((row) => (
              <div
                key={row.index}
                data-index={row.index}
                ref={virtualizer.measureElement}
                className="absolute left-0 top-0 w-full whitespace-pre-wrap px-4"
                style={{ transform: `translateY(${row.start}px)` }}
              >
                <AnsiLine content={lineAt(row.index)} />
              </div>
            ))}
          </div>
        )}
      </div>
      {/* Floating "jump to bottom" pill — visible when the user has
          scrolled up and auto-follow is off. Clicking re-enables
          following and scrolls to the latest line. */}
      {stopped && lines.length > 0 && (
        <button
          type="button"
          onClick={jumpToBottom}
          className="absolute bottom-3 right-5 z-10 rounded-full bg-blue-600 p-1.5 text-white shadow-lg hover:bg-blue-700 dark:bg-blue-500 dark:hover:bg-blue-600"
          title="Jump to latest"
          aria-label="Jump to latest"
        >
          <ArrowDown className="size-4" />
        </button>
      )}
    </div>
  )

  // ─── Layout ────────────────────────────────────────────────────
  if (fullscreen) {
    return (
      <div className="fixed inset-0 z-40 flex flex-col bg-white dark:bg-gray-900">
        {header}
        {banners}
        {logViewport}
      </div>
    )
  }

  return (
    <div className="flex flex-col overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      {header}
      {banners}
      {logViewport}
    </div>
  )
}
