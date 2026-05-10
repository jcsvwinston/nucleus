// useStreamEvents drives ControlService.StreamEvents on behalf of a
// page. It maintains a bounded ring of recent events, exposes
// connection state, and lets the consumer pause/resume the live feed.
//
// Key design choices:
//
//   - Pause is tracked in a ref, not in state, so toggling pause does
//     NOT re-open the upstream stream. The async iterator keeps reading
//     and just discards events while paused.
//
//   - Reconnect is automatic on transport errors with a small exponential
//     backoff (1s, 2s, 4s, capped at 5s). We don't surface that on the
//     UI beyond "connected: false" — this is observability, brief blips
//     are not interesting.
//
//   - The hook accepts a stable `filter` reference. Callers that build
//     filters inline must memoize them, or every render re-opens the
//     upstream call.

import { useEffect, useRef, useState } from 'react'
import { ConnectError, Code } from '@connectrpc/connect'
import { controlClient } from '@/lib/transport'
import { Filter, type Event } from '@/gen/nucleus/admin/v1/admin_pb'

export interface UseStreamEventsOptions {
  filter: Filter
  bufferSize?: number
  includeRecent?: boolean
}

export interface UseStreamEventsResult {
  events: Event[]
  connected: boolean
  paused: boolean
  setPaused: (v: boolean) => void
  clear: () => void
  errorMessage: string | null
}

export function useStreamEvents(opts: UseStreamEventsOptions): UseStreamEventsResult {
  const bufferSize = opts.bufferSize ?? 200
  const [events, setEvents] = useState<Event[]>([])
  const [connected, setConnected] = useState(false)
  const [paused, setPausedState] = useState(false)
  const [errorMessage, setErrorMessage] = useState<string | null>(null)

  const pausedRef = useRef(false)
  const setPaused = (v: boolean): void => {
    pausedRef.current = v
    setPausedState(v)
  }

  useEffect(() => {
    const ctrl = new AbortController()
    let alive = true
    let backoffMs = 1000

    async function loop(): Promise<void> {
      while (alive) {
        try {
          setConnected(true)
          setErrorMessage(null)
          const stream = controlClient.streamEvents(
            {
              filter: opts.filter,
              includeRecent: opts.includeRecent ?? true,
            },
            { signal: ctrl.signal },
          )
          for await (const ev of stream) {
            if (!alive) return
            if (pausedRef.current) continue
            setEvents((prev) => {
              const next = [ev, ...prev]
              if (next.length > bufferSize) next.length = bufferSize
              return next
            })
          }
          // Stream finished cleanly (server-initiated). Reconnect.
          backoffMs = 1000
        } catch (err) {
          if (!alive) return
          if (err instanceof ConnectError && err.code === Code.Canceled) {
            // Local cancel; the effect cleanup is the only place that
            // aborts our controller intentionally.
            return
          }
          setErrorMessage(err instanceof Error ? err.message : String(err))
        } finally {
          if (alive) setConnected(false)
        }

        if (!alive) return
        await sleep(backoffMs)
        backoffMs = Math.min(5000, backoffMs * 2)
      }
    }

    void loop()
    return () => {
      alive = false
      ctrl.abort()
    }
  }, [opts.filter, opts.includeRecent, bufferSize])

  return {
    events,
    connected,
    paused,
    setPaused,
    clear: () => setEvents([]),
    errorMessage,
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}
