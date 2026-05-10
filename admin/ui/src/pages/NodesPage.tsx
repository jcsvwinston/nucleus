import { useNodes } from '@/hooks/useNodes'
import { PageBody, PageHeader } from '@/components/Layout'
import { formatRelative, timestampToDate } from '@/lib/format'

export function NodesPage() {
  const { nodes, isLoading, isError, error, refetch } = useNodes()

  return (
    <>
      <PageHeader
        title="Connected Nodes"
        subtitle="Framework processes registered with this admin server."
        actions={
          <button
            type="button"
            onClick={refetch}
            className="rounded-md border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-sm text-zinc-200 hover:bg-zinc-800"
          >
            Refresh
          </button>
        }
      />
      <PageBody>
        {isError && (
          <div className="mb-4 rounded-md border border-rose-800 bg-rose-950/50 px-4 py-3 text-sm text-rose-300">
            Failed to load: {error?.message ?? 'unknown error'}
          </div>
        )}
        <div className="overflow-hidden rounded-lg border border-zinc-800">
          <table className="w-full text-sm">
            <thead className="bg-zinc-900/60 text-left text-xs uppercase tracking-wider text-zinc-500">
              <tr>
                <th className="px-4 py-2 font-medium">Node ID</th>
                <th className="px-4 py-2 font-medium">Version</th>
                <th className="px-4 py-2 font-medium">Started</th>
                <th className="px-4 py-2 font-medium">Last Seen</th>
                <th className="px-4 py-2 font-medium">Labels</th>
                <th className="px-4 py-2 font-medium text-right">Status</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-zinc-800">
              {nodes.length === 0 && (
                <tr>
                  <td colSpan={6} className="px-4 py-8 text-center text-zinc-500">
                    {isLoading ? 'Loading…' : 'No nodes connected.'}
                  </td>
                </tr>
              )}
              {nodes.map((n) => (
                <tr key={n.nodeId} className="hover:bg-zinc-900/40">
                  <td className="px-4 py-2 font-mono text-xs">{n.nodeId}</td>
                  <td className="px-4 py-2 text-zinc-300">{n.version || '—'}</td>
                  <td className="px-4 py-2 text-zinc-400">
                    {formatRelative(timestampToDate(n.startedAt))}
                  </td>
                  <td className="px-4 py-2 text-zinc-400">
                    {formatRelative(timestampToDate(n.lastSeenAt))}
                  </td>
                  <td className="px-4 py-2">
                    <Labels labels={n.labels} />
                  </td>
                  <td className="px-4 py-2 text-right">
                    <StatusBadge connected={n.connected} />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </PageBody>
    </>
  )
}

function Labels(props: { labels: { [k: string]: string } }) {
  const entries = Object.entries(props.labels)
  if (entries.length === 0) return <span className="text-zinc-600">—</span>
  return (
    <div className="flex flex-wrap gap-1">
      {entries.map(([k, v]) => (
        <span
          key={k}
          className="rounded bg-zinc-800 px-1.5 py-0.5 font-mono text-xs text-zinc-300"
        >
          {k}={v}
        </span>
      ))}
    </div>
  )
}

function StatusBadge(props: { connected: boolean }) {
  return (
    <span
      className={[
        'inline-block rounded-full px-2 py-0.5 text-xs',
        props.connected
          ? 'bg-emerald-900/40 text-emerald-300 ring-1 ring-emerald-700'
          : 'bg-zinc-800 text-zinc-400 ring-1 ring-zinc-700',
      ].join(' ')}
    >
      {props.connected ? 'Online' : 'Offline'}
    </span>
  )
}
