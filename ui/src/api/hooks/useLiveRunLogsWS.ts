import { useEffect, useRef, useState } from 'react'
import { loadRuntimeConfig } from '@/config/runtime'

// useLiveRunLogsWS opens a WebSocket to the API's live-run log stream
// endpoint for a single run. The API sends:
//   - `{type:"snapshot", text, truncated?}` once on connect (buffer)
//   - `{type:"log", text}` as new chunks arrive
//   - `{type:"truncated"}` when the ring buffer has evicted older bytes
//   - `{type:"run_ended"}` when the run is deleted / runner says bye
//
// PERFORMANCE NOTE: the accumulated log text lives in a plain ref (not
// React state) to avoid V8 GC pressure from React holding old + new
// copies of a ≥500 KB string during reconciliation. A lightweight
// `version` counter drives re-renders instead. WS messages are batched
// via requestAnimationFrame so at most ~60 flushes/sec hit React.

interface LogsMessage {
  type: 'snapshot' | 'log' | 'truncated' | 'run_ended'
  text?: string
  truncated?: boolean
}

export interface LiveRunLogsState {
  /** Ref to the accumulated text buffer. Read .current in render. */
  textRef: React.RefObject<string>
  /** Bumped on every flush — lets consumers know text changed. */
  version: number
  truncated: boolean
  ended: boolean
  connected: boolean
}

const MAX_BACKOFF_MS = 30_000
const INITIAL_BACKOFF_MS = 1_000
// Client-side cap so the browser doesn't OOM on a long session. The
// server caps at 512 KB; we keep a bit more so a reconnect snapshot
// plus subsequent deltas have headroom before we start trimming.
const MAX_CLIENT_TEXT_BYTES = 1024 * 1024

export function useLiveRunLogsWS(
  runId: string | undefined,
  enabled: boolean,
): LiveRunLogsState {
  // Text buffer lives in a ref — React never diffs or holds two
  // copies of it. A monotonic version counter in state triggers
  // re-renders when text changes.
  const textRef = useRef('')
  const [version, setVersion] = useState(0)
  const [truncated, setTruncated] = useState(false)
  const [ended, setEnded] = useState(false)
  const [connected, setConnected] = useState(false)

  const wsRef = useRef<WebSocket | null>(null)
  const generationRef = useRef(0)

  useEffect(() => {
    if (!enabled || !runId) {
      return
    }

    const myGeneration = ++generationRef.current

    let cancelled = false
    let backoff = INITIAL_BACKOFF_MS
    let reconnectTimer: number | undefined
    let activeRafId = 0

    // Reset for a fresh connection.
    textRef.current = ''
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setVersion((v) => v + 1)
     
    setTruncated(false)
     
    setEnded(false)
     
    setConnected(false)

    const connect = async () => {
      if (cancelled || generationRef.current !== myGeneration) return

      const config = await loadRuntimeConfig()
      if (cancelled || generationRef.current !== myGeneration) return

      const baseUrl = config.api?.baseUrl
      if (!baseUrl) return

      const wsUrl =
        baseUrl.replace(/^http:/, 'ws:').replace(/^https:/, 'wss:') +
        `/api/v1/index/live_runs/${encodeURIComponent(runId)}/logs/ws`

      const ws = new WebSocket(wsUrl)
      wsRef.current = ws

      // ── rAF batching ────────────────────────────────────────
      // Accumulate WS messages in plain variables and flush into
      // the ref + bump the version counter once per animation
      // frame. This limits React renders to ~60/sec and avoids
      // creating a new multi-hundred-KB string in React state on
      // every message.
      let pendingText = ''
      let pendingSnapshot: { text: string; truncated: boolean } | null = null
      let pendingTruncated = false
      let pendingEnded = false

      const flushPending = () => {
        activeRafId = 0
        if (generationRef.current !== myGeneration) return

        if (pendingSnapshot) {
          textRef.current = pendingSnapshot.text
          setTruncated(pendingSnapshot.truncated)
          pendingSnapshot = null
        }

        if (pendingText) {
          textRef.current += pendingText
          pendingText = ''
        }

        // Client-side cap — trim from the front if we've grown past
        // the limit so the browser doesn't OOM on a very long run.
        if (textRef.current.length > MAX_CLIENT_TEXT_BYTES) {
          const overflow = textRef.current.length - MAX_CLIENT_TEXT_BYTES
          // Find the next newline after the trim point so we don't
          // break a line in the middle.
          const nextNl = textRef.current.indexOf('\n', overflow)
          textRef.current = nextNl >= 0
            ? textRef.current.slice(nextNl + 1)
            : textRef.current.slice(overflow)
          setTruncated(true)
        }

        if (pendingTruncated) {
          setTruncated(true)
          pendingTruncated = false
        }

        if (pendingEnded) {
          setEnded(true)
          pendingEnded = false
        }

        setVersion((v) => v + 1)
      }

      const scheduleFlush = () => {
        if (!activeRafId) activeRafId = requestAnimationFrame(flushPending)
      }

      ws.onopen = () => {
        if (generationRef.current !== myGeneration) {
          ws.close()
          return
        }

        backoff = INITIAL_BACKOFF_MS
        setConnected(true)
      }

      ws.onmessage = (event) => {
        if (generationRef.current !== myGeneration) return

        let msg: LogsMessage
        try {
          msg = JSON.parse(event.data as string) as LogsMessage
        } catch {
          return
        }

        switch (msg.type) {
          case 'snapshot':
            pendingText = ''
            pendingSnapshot = { text: msg.text ?? '', truncated: !!msg.truncated }
            break
          case 'log':
            pendingText += msg.text ?? ''
            break
          case 'truncated':
            pendingTruncated = true
            break
          case 'run_ended':
            pendingEnded = true
            break
          default:
            return
        }

        scheduleFlush()
      }

      ws.onerror = () => {
        // Surfaced via onclose.
      }

      ws.onclose = () => {
        if (generationRef.current !== myGeneration) return

        setConnected(false)
        wsRef.current = null

        if (cancelled) return

        reconnectTimer = window.setTimeout(() => {
          if (!cancelled && generationRef.current === myGeneration) {
            void connect()
          }
        }, backoff)
        backoff = Math.min(backoff * 2, MAX_BACKOFF_MS)
      }
    }

    void connect()

    return () => {
      cancelled = true
      if (activeRafId) cancelAnimationFrame(activeRafId)
      if (reconnectTimer !== undefined) {
        window.clearTimeout(reconnectTimer)
      }
      if (wsRef.current) {
        wsRef.current.close()
        wsRef.current = null
      }
    }
  }, [runId, enabled])

  return { textRef, version, truncated, ended, connected }
}
