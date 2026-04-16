import { useEffect, useRef, useState } from 'react'
import { loadRuntimeConfig } from '@/config/runtime'

// useLiveRunLogsWS opens a WebSocket to the API's live-run log stream
// endpoint for a single run. The API sends:
//   - `{type:"snapshot", text, truncated?}` once on connect (current buffer)
//   - `{type:"log", text}` as new chunks arrive
//   - `{type:"truncated"}` when the ring buffer has evicted older bytes
//   - `{type:"run_ended"}` when the run is deleted / the runner says bye
//
// The hook manages its own reconnect loop (1 s → 30 s backoff) while
// `enabled` is true. When `enabled` flips to false (panel collapsed)
// or `runId` changes, the current WS is closed and buffers reset.
//
// No React Query — streaming semantics don't fit a full-replace cache.

interface LogsMessage {
  type: 'snapshot' | 'log' | 'truncated' | 'run_ended'
  text?: string
  truncated?: boolean
}

interface LiveRunLogsState {
  text: string
  truncated: boolean
  ended: boolean
  connected: boolean
}

const MAX_BACKOFF_MS = 30_000
const INITIAL_BACKOFF_MS = 1_000

export function useLiveRunLogsWS(
  runId: string | undefined,
  enabled: boolean,
): LiveRunLogsState {
  const [state, setState] = useState<LiveRunLogsState>({
    text: '',
    truncated: false,
    ended: false,
    connected: false,
  })

  // Ref to the active WebSocket so we can close it on cleanup, and
  // avoid stale-closure issues with React's effect timing.
  const wsRef = useRef<WebSocket | null>(null)
  // Guard against late messages from a WS we're already replacing.
  const generationRef = useRef(0)

  useEffect(() => {
    if (!enabled || !runId) {
      return
    }

    const myGeneration = ++generationRef.current

    let cancelled = false
    let backoff = INITIAL_BACKOFF_MS
    let reconnectTimer: number | undefined

    // Reset state for a fresh connection (new runId or re-enable). One
    // intentional cascading render — this is a reset signal bounded
    // per enable/runId change, not steady-state derived data.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setState({ text: '', truncated: false, ended: false, connected: false })

    const connect = async () => {
      if (cancelled || generationRef.current !== myGeneration) return

      const config = await loadRuntimeConfig()
      if (cancelled || generationRef.current !== myGeneration) return

      const baseUrl = config.api?.baseUrl
      if (!baseUrl) {
        // No API configured — nothing to connect to.
        return
      }

      const wsUrl =
        baseUrl.replace(/^http:/, 'ws:').replace(/^https:/, 'wss:') +
        `/api/v1/index/live_runs/${encodeURIComponent(runId)}/logs/ws`

      const ws = new WebSocket(wsUrl)
      wsRef.current = ws

      ws.onopen = () => {
        if (generationRef.current !== myGeneration) {
          ws.close()
          return
        }

        backoff = INITIAL_BACKOFF_MS
        setState((s) => ({ ...s, connected: true }))
      }

      ws.onmessage = (event) => {
        if (generationRef.current !== myGeneration) return

        let msg: LogsMessage
        try {
          msg = JSON.parse(event.data as string) as LogsMessage
        } catch {
          return
        }

        setState((s) => {
          switch (msg.type) {
            case 'snapshot':
              return {
                ...s,
                text: msg.text ?? '',
                truncated: !!msg.truncated,
              }
            case 'log':
              return { ...s, text: s.text + (msg.text ?? '') }
            case 'truncated':
              return { ...s, truncated: true }
            case 'run_ended':
              return { ...s, ended: true }
            default:
              return s
          }
        })
      }

      ws.onerror = () => {
        // Surfaced via onclose; nothing to do here directly.
      }

      ws.onclose = () => {
        if (generationRef.current !== myGeneration) return

        setState((s) => ({ ...s, connected: false }))
        wsRef.current = null

        if (cancelled) return

        // Exponential backoff reconnect while still enabled.
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
      if (reconnectTimer !== undefined) {
        window.clearTimeout(reconnectTimer)
      }
      if (wsRef.current) {
        wsRef.current.close()
        wsRef.current = null
      }
    }
  }, [runId, enabled])

  return state
}
