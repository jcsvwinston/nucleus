import { useMemo } from 'react'
import { useNodes } from '@/hooks/useNodes'
import { PageBody, PageHeader } from '@/components/Layout'

export function DashboardPage() {
  const { nodes, isLoading } = useNodes()

  const stats = useMemo(() => {
    const total = nodes.length
    const online = nodes.filter((n) => n.connected).length
    const versions = new Set(nodes.map((n) => n.version || 'unknown'))
    return { total, online, versions: versions.size }
  }, [nodes])

  return (
    <>
      <PageHeader
        title="Dashboard"
        subtitle="Real-time view of the Nucleus fleet served by this admin server."
      />
      <PageBody>
        <div className="grid grid-cols-1 gap-4 md:grid-cols-3">
          <Stat label="Connected nodes" value={stats.online} loading={isLoading} />
          <Stat label="Total registered" value={stats.total} loading={isLoading} />
          <Stat label="Distinct versions" value={stats.versions} loading={isLoading} />
        </div>
        <div className="mt-8 rounded-lg border border-zinc-800 bg-zinc-900/40 p-6 text-sm text-zinc-400">
          <p>
            Use the sidebar to inspect live HTTP traffic, SQL statements, and session
            activity across the fleet.
          </p>
          <p className="mt-2">
            Each stream carries pre-sanitized payloads: request bodies are summarized,
            SQL argument values are masked, and session tokens are truncated to a
            non-reversible prefix.
          </p>
        </div>
      </PageBody>
    </>
  )
}

function Stat(props: { label: string; value: number; loading: boolean }) {
  return (
    <div className="rounded-lg border border-zinc-800 bg-zinc-900/40 p-6">
      <div className="text-xs uppercase tracking-wider text-zinc-500">{props.label}</div>
      <div className="mt-2 text-3xl font-semibold tabular-nums">
        {props.loading ? <span className="text-zinc-700">—</span> : props.value.toLocaleString()}
      </div>
    </div>
  )
}
