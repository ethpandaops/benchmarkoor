import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import {
  ScrollText,
  AlertTriangle,
  Maximize2,
  Pause,
  Play,
  Plug,
  Unplug,
  X,
} from 'lucide-react'
import clsx from 'clsx'
import { useLiveRunLogsWS } from '@/api/hooks/useLiveRunLogsWS'
import { AnsiLine } from '@/components/shared/AnsiLine'

interface LiveRunLogPanelProps {
  runId: string
  // Optional context shown in the fullscreen header so the user knows
  // which run's logs they're staring at when the rest of the page is
  // covered. In the inline (non-fullscreen) view the surrounding page
  // already provides all this context, so we keep that title minimal.
  client?: string
  instanceId?: string
}

/**
 * LiveRunLogPanel streams the runner's benchmarkoor.log over a
 * WebSocket and renders each line with ANSI coloring. The panel is
 * always mounted on the live view (so the runner gets told to start
 * streaming as soon as someone lands on the page), with an optional
 * fullscreen mode for long debugging sessions.
 */
// AUTO_STOP_MS is how long an open log stream stays connected without
// an explicit reconnect. The goal is to make sure a tab someone opens
// and forgets about doesn't keep a WS (and the runner's log tailer)
// alive forever. Users can reconnect at any time.
const AUTO_STOP_MS = 5 * 60 * 1000

export function LiveRunLogPanel({ runId, client, instanceId }: LiveRunLogPanelProps) {
  const [fullscreen, setFullscreen] = useState(false)
  // Pause freezes the display but keeps the WS open — the runner
  // keeps streaming, the API keeps buffering, and Resume catches up.
  const [paused, setPaused] = useState(false)
  const [frozenText, setFrozenText] = useState('')
  // Stop fully disconnects: the UI WS closes, the API's subscriber
  // count drops to zero, and the runner receives stream_off. Zero
  // bytes in flight. Reconnect re-opens the WS and picks up the
  // current API buffer as a fresh snapshot.
  const [stopped, setStopped] = useState(false)
  // Absolute wall-clock deadline for the auto-stop timer. Used both
  // to fire the stop and to drive the countdown label in the header.
  const [autoStopAt, setAutoStopAt] = useState<number | null>(() => Date.now() + AUTO_STOP_MS)
  const [now, setNow] = useState(() => Date.now())

  const { text, truncated, ended, connected } = useLiveRunLogsWS(runId, !stopped)

  // Tick once per second while connected, so the countdown label
  // refreshes. Cheap (one setInterval), and it stops ticking the
  // moment the stream is stopped.
  useEffect(() => {
    if (stopped || autoStopAt === null) return

    const id = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [stopped, autoStopAt])

  // Fire the auto-stop exactly at the deadline.
  useEffect(() => {
    if (stopped || autoStopAt === null) return

    const remaining = autoStopAt - Date.now()
    if (remaining <= 0) {
      // Deadline already passed (e.g. user returned to a long-idle
      // tab and the computed deadline is in the past). One bounded
      // follow-up render.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setStopped(true)
      return
    }

    const id = window.setTimeout(() => setStopped(true), remaining)
    return () => window.clearTimeout(id)
  }, [stopped, autoStopAt])

  // When paused, render the snapshot we captured; otherwise render
  // the live buffer. The pending counter shows how many new bytes
  // have arrived since pause so the user knows they're behind.
  const displayText = paused ? frozenText : text
  const pendingBytes = paused ? Math.max(0, text.length - frozenText.length) : 0
  const pendingLines = useMemo(() => {
    if (!paused) return 0
    const delta = text.slice(frozenText.length)
    if (!delta) return 0
    // Count newlines in the delta; clamp at 1 so a partial line still
    // reads as "at least 1 new line".
    let count = 0
    for (const c of delta) if (c === '\n') count++
    return Math.max(count, 1)
  }, [paused, text, frozenText])

  // Split the rolling buffer into lines once per render so the JSX
  // below stays declarative. The trailing-empty-string case (from a
  // final "\n") is trimmed so we don't render a phantom blank row.
  const lines = useMemo(() => {
    if (!displayText) return []
    const parts = displayText.split('\n')
    if (parts.length > 0 && parts[parts.length - 1] === '') parts.pop()
    return parts
  }, [displayText])

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
  }, [displayText])

  // Fullscreen UX: ESC to exit; lock body scroll so the page behind
  // doesn't scroll with the overlay open. Mirrors the pattern used by
  // the suite-detail heatmaps.
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

  // When switching modes, the new scroll container needs to start
  // pinned to the bottom (otherwise the first paint can leave it at 0).
  useEffect(() => {
    wasAtBottomRef.current = true
  }, [fullscreen])

  // Three-way status badge: stopped > (connected/connecting) > paused.
  // Paused is reflected separately via the pendingLines counter so we
  // don't clutter this badge with it.
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

  const connectedBadge = (
    <span
      className={clsx(
        'ml-1 inline-flex items-center gap-1 rounded-xs px-1.5 py-0.5 text-xs font-medium',
        statusBadge.wrap,
      )}
    >
      <span className={clsx('size-1.5 rounded-full', statusBadge.dot)} aria-hidden="true" />
      {statusBadge.text}
    </span>
  )

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
        {/* Fullscreen mode covers the surrounding page context, so we
            surface the client logo + instance + run id right here. In
            the inline view those are already visible in the page
            breadcrumb above, so we keep the title minimal. */}
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
        {connectedBadge}
        {/* Countdown for the auto-disconnect. Rendered in the header
            so the stop is never surprising. Hidden when stopped or
            when the deadline is unset. */}
        {!stopped && autoStopAt !== null && (() => {
          const remainingMs = Math.max(0, autoStopAt - now)
          const totalSec = Math.ceil(remainingMs / 1000)
          const mm = Math.floor(totalSec / 60)
          const ss = totalSec % 60
          const label = mm > 0 ? `${mm}m ${ss.toString().padStart(2, '0')}s` : `${ss}s`
          return (
            <span
              className="inline-flex items-center rounded-xs bg-gray-100 px-1.5 py-0.5 text-xs font-medium text-gray-600 dark:bg-gray-700 dark:text-gray-300"
              title="The log stream auto-disconnects after 5 minutes. Click reconnect to extend."
            >
              auto-stops in {label}
            </span>
          )
        })()}
        {paused && pendingLines > 0 && (
          <span
            className="inline-flex items-center gap-1 rounded-xs bg-amber-100 px-1.5 py-0.5 text-xs font-medium text-amber-800 dark:bg-amber-900/40 dark:text-amber-200"
            title={`${pendingBytes} new byte(s) buffered since pause`}
          >
            +{pendingLines} new
          </span>
        )}
      </h3>
      <div className="flex shrink-0 items-center gap-2">
        {/* Pause / Resume — only meaningful while connected to the
            stream; hidden entirely when stopped. */}
        {!stopped && (
          <button
            type="button"
            onClick={() => {
              if (paused) {
                // Resume: drop the snapshot, fall back to live buffer,
                // and make sure auto-scroll snaps to the bottom.
                setPaused(false)
                setFrozenText('')
                wasAtBottomRef.current = true
              } else {
                // Pause: capture the current buffer so the panel
                // renders a stable view while new bytes keep arriving.
                setFrozenText(text)
                setPaused(true)
              }
            }}
            className="rounded-xs border border-gray-300 bg-white px-2 py-1 text-sm/6 text-gray-600 hover:bg-gray-50 dark:border-gray-600 dark:bg-gray-700 dark:text-gray-300 dark:hover:bg-gray-600"
            title={paused ? 'Resume (catch up to latest)' : 'Pause (freeze the current view)'}
            aria-label={paused ? 'Resume log stream' : 'Pause log stream'}
          >
            {paused ? <Play className="size-4" /> : <Pause className="size-4" />}
          </button>
        )}
        {/* Stop / Reconnect — hard toggle that closes the WS entirely.
            When stopped the pause state is force-cleared (pause is
            moot without a live feed); reconnect snaps auto-scroll to
            the bottom so the fresh snapshot renders correctly. */}
        <button
          type="button"
          onClick={() => {
            if (stopped) {
              // Reconnect: arm a fresh auto-stop deadline.
              setStopped(false)
              setAutoStopAt(Date.now() + AUTO_STOP_MS)
              wasAtBottomRef.current = true
            } else {
              // Explicit stop: cancel any pending auto-stop.
              setStopped(true)
              setAutoStopAt(null)
              setPaused(false)
              setFrozenText('')
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

  const logPre = (
    <pre
      ref={scrollRef}
      onScroll={handleScroll}
      className={clsx(
        'overflow-auto whitespace-pre-wrap bg-gray-950 px-4 py-3 font-mono text-xs/5 text-gray-100',
        fullscreen ? 'min-h-0 flex-1' : 'h-64 max-h-[50vh]',
      )}
    >
      {lines.length === 0 ? (
        <span className="text-gray-500">
          {stopped
            ? 'Stream stopped. Click the reconnect button to resume.'
            : connected
              ? 'Waiting for log output…'
              : 'Connecting…'}
        </span>
      ) : (
        lines.map((line, i) => (
          <div key={i}>
            <AnsiLine content={line} />
          </div>
        ))
      )}
    </pre>
  )

  if (fullscreen) {
    return (
      <div className="fixed inset-0 z-40 flex flex-col bg-white dark:bg-gray-900">
        {header}
        {banners}
        {logPre}
      </div>
    )
  }

  return (
    <div className="flex flex-col overflow-hidden rounded-sm bg-white shadow-xs dark:bg-gray-800">
      {header}
      {banners}
      {logPre}
    </div>
  )
}
