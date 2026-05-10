import { useMemo } from 'react'
import { PageBody, PageHeader } from '@/components/Layout'
import { StreamControls } from '@/components/StreamControls'
import { useStreamEvents } from '@/hooks/useStreamEvents'
import {
  Filter,
  EventType,
  SessionChangeEvent_Kind,
} from '@/gen/nucleus/admin/v1/admin_pb'
import { formatTime, timestampToDate } from '@/lib/format'

export function SessionsPage() {
  const filter = useMemo(
    () => new Filter({ types: [EventType.SESSION_CHANGE] }),
    [],
  )

  const stream = useStreamEvents({ filter, bufferSize: 200, includeRecent: true })

  return (
    <>
      <PageHeader
        title="Session activity"
        subtitle="Created / touched / destroyed lifecycle events across the fleet. Tokens are non-reversible prefixes."
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
                <th className="px-3 py-2 font-medium">Kind</th>
                <th className="px-3 py-2 font-medium">User</th>
                <th className="px-3 py-2 font-medium">Token (short)</th>
                <th className="px-3 py-2 font-medium">Last Route</th>
                <th className="px-3 py-2 font-medium">IP</th>
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
                if (ev.body.case !== 'sessionChange') return null
                const s = ev.body.value
                return (
                  <tr key={`${ev.nodeId}-${idx}`} className="hover:bg-zinc-900/40">
                    <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-zinc-500">
                      {formatTime(timestampToDate(ev.timestamp))}
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-zinc-400">
                      {ev.nodeId}
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5 text-xs">
                      <KindBadge kind={s.kind} />
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-zinc-200">
                      {s.userId || '—'}
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-zinc-400">
                      {s.tokenShort || '—'}
                    </td>
                    <td className="px-3 py-1.5 font-mono text-xs text-zinc-300">
                      {s.lastRoute || '—'}
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-zinc-500">
                      {s.ip || '—'}
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

function KindBadge(props: { kind: SessionChangeEvent_Kind }) {
  const { label, klass } = describeKind(props.kind)
  return (
    <span className={['rounded-full px-2 py-0.5 text-xs', klass].join(' ')}>{label}</span>
  )
}

function describeKind(k: SessionChangeEvent_Kind): { label: string; klass: string } {
  switch (k) {
    case SessionChangeEvent_Kind.CREATED:
      return { label: 'created', klass: 'bg-emerald-900/40 text-emerald-300 ring-1 ring-emerald-700' }
    case SessionChangeEvent_Kind.TOUCHED:
      return { label: 'touched', klass: 'bg-sky-900/40 text-sky-300 ring-1 ring-sky-700' }
    case SessionChangeEvent_Kind.DESTROYED:
      return { label: 'destroyed', klass: 'bg-rose-900/40 text-rose-300 ring-1 ring-rose-700' }
    default:
      return { label: 'unspecified', klass: 'bg-zinc-800 text-zinc-400 ring-1 ring-zinc-700' }
  }
}
