import { useMemo } from 'react'
import { PageBody, PageHeader } from '@/components/Layout'
import { StreamControls } from '@/components/StreamControls'
import { useStreamEvents } from '@/hooks/useStreamEvents'
import { Filter, EventType } from '@/gen/nucleus/admin/v1/admin_pb'
import {
  durationToMillis,
  formatDuration,
  formatTime,
  methodClass,
  statusClass,
  timestampToDate,
} from '@/lib/format'

export function HTTPStreamPage() {
  // The filter must be referentially stable so useStreamEvents does not
  // re-open on every render.
  const filter = useMemo(
    () => new Filter({ types: [EventType.HTTP_REQUEST] }),
    [],
  )

  const stream = useStreamEvents({ filter, bufferSize: 300, includeRecent: true })

  return (
    <>
      <PageHeader
        title="HTTP requests"
        subtitle="Live stream from every connected agent. Sensitive query params are starred at the source."
        actions={
          <StreamControls
            connected={stream.connected}
            paused={stream.paused}
            onTogglePause={() => stream.setPaused(!stream.paused)}
            onClear={stream.clear}
            count={stream.events.length}
            error={stream.errorMessage}
          />
        }
      />
      <PageBody>
        <div className="overflow-hidden rounded-lg border border-zinc-800">
          <table className="w-full text-sm">
            <thead className="bg-zinc-900/60 text-left text-xs uppercase tracking-wider text-zinc-500">
              <tr>
                <th className="px-3 py-2 font-medium">Time</th>
                <th className="px-3 py-2 font-medium">Node</th>
                <th className="px-3 py-2 font-medium">Method</th>
                <th className="px-3 py-2 font-medium">Path</th>
                <th className="px-3 py-2 font-medium text-right">Status</th>
                <th className="px-3 py-2 font-medium text-right">Duration</th>
                <th className="px-3 py-2 font-medium">Remote</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-800">
              {stream.events.length === 0 && (
                <tr>
                  <td colSpan={7} className="px-3 py-8 text-center text-zinc-500">
                    Waiting for events…
                  </td>
                </tr>
              )}
              {stream.events.map((ev, idx) => {
                if (ev.body.case !== 'httpRequest') return null
                const http = ev.body.value
                return (
                  <tr key={`${ev.nodeId}-${idx}`} className="hover:bg-zinc-900/40">
                    <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-zinc-500">
                      {formatTime(timestampToDate(ev.timestamp))}
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-zinc-400">
                      {ev.nodeId}
                    </td>
                    <td className={['px-3 py-1.5 font-mono text-xs', methodClass(http.method)].join(' ')}>
                      {http.method}
                    </td>
                    <td className="px-3 py-1.5 font-mono text-xs text-zinc-200">
                      {http.path}
                    </td>
                    <td
                      className={[
                        'whitespace-nowrap px-3 py-1.5 text-right font-mono text-xs',
                        statusClass(http.status),
                      ].join(' ')}
                    >
                      {http.status}
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5 text-right text-xs text-zinc-300 tabular-nums">
                      {formatDuration(durationToMillis(http.duration))}
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-zinc-500">
                      {http.remoteIp}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      </PageBody>
    </>
  )
}
