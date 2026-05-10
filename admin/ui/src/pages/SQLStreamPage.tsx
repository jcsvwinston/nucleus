import { useMemo } from 'react'
import { PageBody, PageHeader } from '@/components/Layout'
import { StreamControls } from '@/components/StreamControls'
import { useStreamEvents } from '@/hooks/useStreamEvents'
import { Filter, EventType } from '@/gen/nucleus/admin/v1/admin_pb'
import { durationToMillis, formatDuration, formatTime, timestampToDate } from '@/lib/format'

export function SQLStreamPage() {
  const filter = useMemo(
    () => new Filter({ types: [EventType.SQL_STATEMENT] }),
    [],
  )

  const stream = useStreamEvents({ filter, bufferSize: 300, includeRecent: true })

  return (
    <>
      <PageHeader
        title="SQL statements"
        subtitle="CRUD-layer queries observed by every connected agent. Argument values are masked at the source."
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
                <th className="px-3 py-2 font-medium">Model</th>
                <th className="px-3 py-2 font-medium">Op</th>
                <th className="px-3 py-2 font-medium">Query</th>
                <th className="px-3 py-2 font-medium text-right">Duration</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-800">
              {stream.events.length === 0 && (
                <tr>
                  <td colSpan={6} className="px-3 py-8 text-center text-zinc-500">
                    Waiting for events…
                  </td>
                </tr>
              )}
              {stream.events.map((ev, idx) => {
                if (ev.body.case !== 'sqlStatement') return null
                const sql = ev.body.value
                return (
                  <tr key={`${ev.nodeId}-${idx}`} className="hover:bg-zinc-900/40">
                    <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-zinc-500">
                      {formatTime(timestampToDate(ev.timestamp))}
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-zinc-400">
                      {ev.nodeId}
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-zinc-200">
                      {sql.modelName || '—'}
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-amber-300">
                      {sql.operation}
                    </td>
                    <td className="px-3 py-1.5 font-mono text-xs text-zinc-300">
                      <div className="max-w-2xl truncate" title={sql.query}>
                        {sql.query}
                      </div>
                      {sql.error !== '' && (
                        <div className="text-rose-400">{sql.error}</div>
                      )}
                    </td>
                    <td className="whitespace-nowrap px-3 py-1.5 text-right text-xs text-zinc-300 tabular-nums">
                      {formatDuration(durationToMillis(sql.duration))}
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
