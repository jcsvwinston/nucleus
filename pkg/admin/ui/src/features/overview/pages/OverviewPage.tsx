import { useEffect, useState } from 'react'
import { Activity, Boxes, Database, Network, PackageCheck, Table } from 'lucide-react'

import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import * as api from '@/services/api'
import type { ModelsResponse, SystemSnapshot } from '@/types'

function formatTimestamp(value?: string): string {
  if (!value) return 'Not available'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function statusVariant(enabled: boolean): 'default' | 'secondary' | 'outline' {
  if (enabled) return 'default'
  return 'secondary'
}

export default function OverviewPage() {
  const [modelsResponse, setModelsResponse] = useState<ModelsResponse | null>(null)
  const [system, setSystem] = useState<SystemSnapshot | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let mounted = true

    const load = async () => {
      try {
        const [modelsData, systemData] = await Promise.all([
          api.getModelsWithRuntime(true),
          api.getSystemSnapshot(),
        ])
        if (!mounted) return
        setModelsResponse(modelsData)
        setSystem(systemData)
      } catch (error) {
        console.error('Failed to load overview:', error)
      } finally {
        if (mounted) {
          setLoading(false)
        }
      }
    }

    load()
    return () => {
      mounted = false
    }
  }, [])

  const runtime = modelsResponse?.runtime
  const models = modelsResponse?.models ?? []
  const queues = system?.jobs.queues ?? []
  const schedules = system?.jobs.schedules ?? []
  const nodes = system?.cluster_nodes ?? []

  const stats = [
    {
      label: 'Models',
      value: runtime?.models_total ?? models.length,
      hint: `${runtime?.records_total ?? 0} rows known`,
      icon: Table,
    },
    {
      label: 'Databases',
      value: system?.databases.length ?? runtime?.databases.length ?? 0,
      hint: `${runtime?.engines.length ?? 0} engine groups`,
      icon: Database,
    },
    {
      label: 'Jobs',
      value: system?.jobs.total_pending ?? 0,
      hint: `${system?.jobs.total_queues ?? 0} queues / ${system?.jobs.total_schedules ?? 0} schedules`,
      icon: PackageCheck,
    },
    {
      label: 'Cluster nodes',
      value: nodes.length,
      hint: system?.cluster.connected ? 'relay connected' : system?.cluster.reason ?? 'relay idle',
      icon: Network,
    },
  ]

  if (loading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary"></div>
      </div>
    )
  }

  if (!modelsResponse || !system) {
    return (
      <div className="py-12 text-center">
        <p className="text-muted-foreground">Failed to load the framework overview.</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-3xl font-bold">Overview</h1>
          <Badge variant={statusVariant(system.enabled)}>Runtime snapshot</Badge>
          <Badge variant={statusVariant(system.jobs.enabled)}>Tasks</Badge>
          <Badge variant={statusVariant(system.outbox.enabled)}>Outbox</Badge>
          <Badge variant={statusVariant(system.cluster.enabled)}>Cluster</Badge>
        </div>
        <p className="text-muted-foreground">
          This panel now tracks the actual GoFrame runtime: data layer, background jobs, outbox delivery, and distributed topology.
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        {stats.map((stat) => (
          <Card key={stat.label}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{stat.label}</CardTitle>
              <stat.icon className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-3xl font-semibold">{stat.value}</div>
              <p className="mt-1 text-xs text-muted-foreground">{stat.hint}</p>
            </CardContent>
          </Card>
        ))}
      </div>

      <div className="grid gap-4 xl:grid-cols-[1.2fr_0.8fr]">
        <Card>
          <CardHeader>
            <CardTitle>Application surface</CardTitle>
            <CardDescription>Registered models and data footprint across configured databases.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {models.length === 0 ? (
              <p className="py-4 text-sm text-muted-foreground">No models are registered yet.</p>
            ) : (
              models.slice(0, 8).map((model) => (
                <div key={model.name} className="flex items-center justify-between gap-4 rounded-lg border border-border px-4 py-3">
                  <div className="min-w-0">
                    <p className="truncate font-medium">{model.name}</p>
                    <p className="truncate text-sm text-muted-foreground">{model.table}</p>
                  </div>
                  <Badge variant="secondary">{model.count ?? 0} rows</Badge>
                </div>
              ))
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Control plane</CardTitle>
            <CardDescription>Core runtime metadata surfaced by the framework.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            <ControlRow label="Environment" value={runtime?.environment || 'unknown'} />
            <ControlRow label="Go runtime" value={`${system.go_version} · ${system.go_os}/${system.go_arch}`} />
            <ControlRow label="Default DB" value={system.databases.find((db) => db.is_default)?.alias ?? 'default'} />
            <ControlRow label="Trace links" value={system.telemetry.trace_links_configured ? 'Configured' : 'Disabled'} />
            <ControlRow label="Outbox table" value={system.outbox.table} />
            <ControlRow label="Generated at" value={formatTimestamp(system.generated_at)} />
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Execution runtime</CardTitle>
            <CardDescription>Queues and periodic work discovered from the tasks runtime.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-3">
              <MiniStat label="Pending" value={system.jobs.total_pending} />
              <MiniStat label="Active" value={system.jobs.total_active} />
              <MiniStat label="Scheduled" value={system.jobs.total_scheduled} />
            </div>

            <div className="space-y-3">
              <p className="text-sm font-medium">Queues</p>
              {queues.length === 0 ? (
                <p className="text-sm text-muted-foreground">{system.jobs.reason ?? 'No queues detected.'}</p>
              ) : (
                queues.slice(0, 5).map((queue) => (
                  <div key={queue.name} className="rounded-lg border border-border px-4 py-3">
                    <div className="flex items-center justify-between gap-3">
                      <div>
                        <p className="font-medium">{queue.name}</p>
                        <p className="text-sm text-muted-foreground">
                          {queue.pending} pending · {queue.active} active · {queue.scheduled} scheduled
                        </p>
                      </div>
                      <Badge variant={queue.paused ? 'secondary' : 'outline'}>{queue.paused ? 'Paused' : 'Running'}</Badge>
                    </div>
                  </div>
                ))
              )}
            </div>

            <div className="space-y-3">
              <p className="text-sm font-medium">Schedules</p>
              {schedules.length === 0 ? (
                <p className="text-sm text-muted-foreground">No periodic schedules registered.</p>
              ) : (
                schedules.slice(0, 5).map((schedule) => (
                  <div key={schedule.id} className="rounded-lg border border-border px-4 py-3">
                    <p className="font-medium">{schedule.task_type}</p>
                    <p className="text-sm text-muted-foreground">{schedule.spec}</p>
                  </div>
                ))
              )}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Delivery and topology</CardTitle>
            <CardDescription>Durable outbox flow and distributed live-cluster surface.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-2">
              <MiniStat label="Outbox pending" value={system.outbox.pending} />
              <MiniStat label="Outbox failed" value={system.outbox.failed} />
              <MiniStat label="Nodes visible" value={nodes.length} />
              <MiniStat label="Cluster published" value={system.cluster.published} />
            </div>

            <div className="rounded-lg border border-border px-4 py-3">
              <div className="flex items-center justify-between gap-3">
                <div>
                  <p className="font-medium">Cluster relay</p>
                  <p className="text-sm text-muted-foreground">
                    {system.cluster.connected ? `Channel ${system.cluster.channel || 'configured'}` : system.cluster.reason || 'Relay not connected'}
                  </p>
                </div>
                <Badge variant={statusVariant(system.cluster.connected)}>{system.cluster.connected ? 'Connected' : 'Idle'}</Badge>
              </div>
            </div>

            <div className="space-y-3">
              {nodes.length === 0 ? (
                <p className="text-sm text-muted-foreground">No cluster nodes observed yet.</p>
              ) : (
                nodes.slice(0, 6).map((node) => (
                  <div key={node.node_id} className="flex items-center justify-between gap-3 rounded-lg border border-border px-4 py-3">
                    <div>
                      <p className="font-medium">{node.node_id}</p>
                      <p className="text-sm text-muted-foreground">
                        {node.requests} HTTP · {node.sql_queries} SQL · {node.sessions} sessions
                      </p>
                    </div>
                    <Badge variant={node.status === 'online' ? 'default' : 'secondary'}>{node.status}</Badge>
                  </div>
                ))
              )}
            </div>
          </CardContent>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Framework direction</CardTitle>
          <CardDescription>Django ergonomics plus Encore-style production runtime, surfaced directly in admin.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 md:grid-cols-3">
          <ControlTile
            icon={Boxes}
            title="Application layer"
            body="Services, repositories, contracts, and admin CRUD are all visible from the same generated baseline."
          />
          <ControlTile
            icon={Activity}
            title="Operational runtime"
            body="Queues, schedules, dead-letter actions, and outbox delivery share one explicit framework runtime."
          />
          <ControlTile
            icon={Network}
            title="Distributed surface"
            body="Signals relay and live cluster topology now show how multiple nodes behave as one system."
          />
        </CardContent>
      </Card>
    </div>
  )
}

function MiniStat({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-lg border border-border px-4 py-3">
      <p className="text-xs uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className="mt-2 text-2xl font-semibold">{value}</p>
    </div>
  )
}

function ControlRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between gap-4 border-b border-border pb-3 last:border-b-0 last:pb-0">
      <span className="text-sm text-muted-foreground">{label}</span>
      <span className="text-right text-sm font-medium">{value}</span>
    </div>
  )
}

function ControlTile({
  icon: Icon,
  title,
  body,
}: {
  icon: typeof Activity
  title: string
  body: string
}) {
  return (
    <div className="rounded-lg border border-border px-4 py-4">
      <Icon className="h-5 w-5 text-muted-foreground" />
      <p className="mt-4 font-medium">{title}</p>
      <p className="mt-2 text-sm text-muted-foreground">{body}</p>
    </div>
  )
}
