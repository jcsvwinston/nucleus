import { useEffect, useState } from 'react'
import { Activity, Cpu, Database, MemoryStick, Network, PackageCheck } from 'lucide-react'
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'

import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import * as api from '@/services/api'
import type { SystemSnapshot } from '@/types'

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const order = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), units.length - 1)
  return `${Math.round((bytes / 1024 ** order) * 100) / 100} ${units[order]}`
}

function formatTimestamp(value?: string): string {
  if (!value) return 'Not available'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

type PulsePoint = {
  time: string
  goroutines: number
  cpu: number
  backlog: number
  outbox: number
}

export default function SystemPulsePage() {
  const [snapshot, setSnapshot] = useState<SystemSnapshot | null>(null)
  const [history, setHistory] = useState<PulsePoint[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let mounted = true

    const fetchSnapshot = async () => {
      try {
        const data = await api.getSystemSnapshot()
        if (!mounted) return

        setSnapshot(data)
        setHistory((prev) => [
          ...prev.slice(-19),
          {
            time: new Date().toLocaleTimeString(),
            goroutines: data.goroutines.count,
            cpu: data.process_cpu_load ?? data.cpu_load ?? 0,
            backlog: data.jobs.total_pending,
            outbox: data.outbox.pending,
          },
        ])
      } catch (error) {
        console.error('Failed to fetch system snapshot:', error)
      } finally {
        if (mounted) {
          setLoading(false)
        }
      }
    }

    fetchSnapshot()
    const interval = window.setInterval(fetchSnapshot, 5000)
    return () => {
      mounted = false
      window.clearInterval(interval)
    }
  }, [])

  if (loading) {
    return (
      <div className="flex h-64 items-center justify-center">
        <div className="h-8 w-8 animate-spin rounded-full border-b-2 border-primary"></div>
      </div>
    )
  }

  if (!snapshot) {
    return (
      <div className="py-12 text-center">
        <p className="text-muted-foreground">Failed to load the system pulse.</p>
      </div>
    )
  }

  const summary = [
    {
      label: 'Goroutines',
      value: snapshot.goroutines.count,
      hint: `${snapshot.gomaxprocs} GOMAXPROCS / ${snapshot.cpus} CPUs`,
      icon: Activity,
    },
    {
      label: 'Heap alloc',
      value: formatBytes(snapshot.memory.heap_alloc_bytes),
      hint: `${snapshot.memory.num_gc} GC cycles`,
      icon: MemoryStick,
    },
    {
      label: 'Queue backlog',
      value: snapshot.jobs.total_pending,
      hint: `${snapshot.jobs.total_queues} queues / ${snapshot.jobs.total_schedules} schedules`,
      icon: PackageCheck,
    },
    {
      label: 'Cluster nodes',
      value: snapshot.cluster_nodes.length,
      hint: snapshot.cluster.connected ? 'relay connected' : snapshot.cluster.reason ?? 'relay idle',
      icon: Network,
    },
  ]

  return (
    <div className="space-y-6">
      <div className="space-y-2">
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-3xl font-bold">System Pulse</h1>
          <Badge variant={snapshot.jobs.enabled ? 'default' : 'secondary'}>Tasks</Badge>
          <Badge variant={snapshot.outbox.enabled ? 'default' : 'secondary'}>Outbox</Badge>
          <Badge variant={snapshot.cluster.connected ? 'default' : 'secondary'}>Cluster relay</Badge>
        </div>
        <p className="text-muted-foreground">
          Live operational view of the Go runtime, SQL pools, background execution, outbox delivery, and distributed node activity.
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        {summary.map((item) => (
          <Card key={item.label}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{item.label}</CardTitle>
              <item.icon className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent>
              <div className="text-3xl font-semibold">{item.value}</div>
              <p className="mt-1 text-xs text-muted-foreground">{item.hint}</p>
            </CardContent>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Runtime trend</CardTitle>
          <CardDescription>Last 20 samples of goroutines, queue backlog, and outbox pressure.</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="h-72">
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={history}>
                <CartesianGrid strokeDasharray="3 3" />
                <XAxis dataKey="time" />
                <YAxis />
                <Tooltip />
                <Line type="monotone" dataKey="goroutines" stroke="hsl(var(--primary))" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="backlog" stroke="#f59e0b" strokeWidth={2} dot={false} />
                <Line type="monotone" dataKey="outbox" stroke="#ef4444" strokeWidth={2} dot={false} />
              </LineChart>
            </ResponsiveContainer>
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Database pools</CardTitle>
            <CardDescription>Connection pressure and runtime availability per configured database alias.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {snapshot.databases.length === 0 ? (
              <p className="text-sm text-muted-foreground">No database pools were reported.</p>
            ) : (
              snapshot.databases.map((pool) => (
                <div key={pool.alias} className="rounded-lg border border-border px-4 py-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <p className="font-medium">{pool.alias}</p>
                      <p className="text-sm text-muted-foreground">
                        {pool.dialect} · {pool.in_use} in use / {pool.idle} idle / {pool.open_connections} open
                      </p>
                    </div>
                    <Badge variant={pool.error ? 'secondary' : 'outline'}>{pool.error ? 'Issue' : 'Healthy'}</Badge>
                  </div>
                </div>
              ))
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Outbox delivery</CardTitle>
            <CardDescription>Transactional outbox state backed by the default SQL database.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-2">
              <MetricTile label="Pending" value={snapshot.outbox.pending} />
              <MetricTile label="Processing" value={snapshot.outbox.processing} />
              <MetricTile label="Delivered" value={snapshot.outbox.delivered} />
              <MetricTile label="Failed" value={snapshot.outbox.failed} />
            </div>
            <div className="rounded-lg border border-border px-4 py-3">
              <p className="font-medium">{snapshot.outbox.table}</p>
              <p className="mt-1 text-sm text-muted-foreground">
                {snapshot.outbox.enabled
                  ? `Oldest pending: ${formatTimestamp(snapshot.outbox.oldest_pending_at)}`
                  : snapshot.outbox.reason || 'Outbox runtime not initialized'}
              </p>
              {snapshot.outbox.last_delivered_at ? (
                <p className="mt-2 text-sm text-muted-foreground">
                  Last delivered at {formatTimestamp(snapshot.outbox.last_delivered_at)}
                </p>
              ) : null}
            </div>
          </CardContent>
        </Card>
      </div>

      <div className="grid gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle>Task runtime</CardTitle>
            <CardDescription>Queue backlog and periodic scheduler registrations discovered at runtime.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-3">
            {snapshot.jobs.queues.length === 0 ? (
              <p className="text-sm text-muted-foreground">{snapshot.jobs.reason || 'No queues detected.'}</p>
            ) : (
              snapshot.jobs.queues.slice(0, 6).map((queue) => (
                <div key={queue.name} className="rounded-lg border border-border px-4 py-3">
                  <div className="flex items-center justify-between gap-3">
                    <div>
                      <p className="font-medium">{queue.name}</p>
                      <p className="text-sm text-muted-foreground">
                        {queue.pending} pending · {queue.active} active · {queue.retry} retry · {queue.archived} archived
                      </p>
                    </div>
                    <Badge variant={queue.paused ? 'secondary' : 'outline'}>{queue.paused ? 'Paused' : 'Running'}</Badge>
                  </div>
                </div>
              ))
            )}

            {snapshot.jobs.schedules.length > 0 ? (
              <div className="space-y-3 pt-2">
                <p className="text-sm font-medium">Registered schedules</p>
                {snapshot.jobs.schedules.slice(0, 4).map((schedule) => (
                  <div key={schedule.id} className="rounded-lg border border-border px-4 py-3">
                    <p className="font-medium">{schedule.task_type}</p>
                    <p className="text-sm text-muted-foreground">{schedule.spec}</p>
                  </div>
                ))}
              </div>
            ) : null}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Distributed topology</CardTitle>
            <CardDescription>Node visibility and relay stats from the admin live-cluster runtime.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="grid gap-3 sm:grid-cols-2">
              <MetricTile label="Published" value={snapshot.cluster.published} />
              <MetricTile label="Received" value={snapshot.cluster.received} />
              <MetricTile label="Dropped" value={snapshot.cluster.dropped} />
              <MetricTile label="Ignored" value={snapshot.cluster.ignored} />
            </div>

            <div className="rounded-lg border border-border px-4 py-3">
              <p className="font-medium">{snapshot.cluster.node_id || 'node-local'}</p>
              <p className="mt-1 text-sm text-muted-foreground">
                {snapshot.cluster.connected
                  ? `Connected on ${snapshot.cluster.channel || 'configured channel'}`
                  : snapshot.cluster.reason || 'Relay is not connected'}
              </p>
            </div>

            <div className="space-y-3">
              {snapshot.cluster_nodes.length === 0 ? (
                <p className="text-sm text-muted-foreground">No cluster nodes observed yet.</p>
              ) : (
                snapshot.cluster_nodes.map((node) => (
                  <div key={node.node_id} className="flex items-center justify-between gap-3 rounded-lg border border-border px-4 py-3">
                    <div className="min-w-0">
                      <p className="truncate font-medium">{node.node_id}</p>
                      <p className="truncate text-sm text-muted-foreground">
                        {node.requests} requests · {node.sql_queries} SQL · {node.sessions} sessions
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
          <CardTitle>Runtime metadata</CardTitle>
          <CardDescription>Fast operational facts straight from the current snapshot.</CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 md:grid-cols-2 xl:grid-cols-4">
          <MetaRow icon={Cpu} label="Process CPU" value={`${(snapshot.process_cpu_load ?? 0).toFixed(2)}%`} />
          <MetaRow icon={Database} label="Trace endpoint" value={snapshot.telemetry.otlp_endpoint || 'Not configured'} />
          <MetaRow icon={MemoryStick} label="Last GC pause" value={`${snapshot.memory.last_pause_ms} ms`} />
          <MetaRow icon={Activity} label="Generated at" value={formatTimestamp(snapshot.generated_at)} />
        </CardContent>
      </Card>
    </div>
  )
}

function MetricTile({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-lg border border-border px-4 py-3">
      <p className="text-xs uppercase tracking-wide text-muted-foreground">{label}</p>
      <p className="mt-2 text-2xl font-semibold">{value}</p>
    </div>
  )
}

function MetaRow({
  icon: Icon,
  label,
  value,
}: {
  icon: typeof Activity
  label: string
  value: string
}) {
  return (
    <div className="rounded-lg border border-border px-4 py-4">
      <Icon className="h-5 w-5 text-muted-foreground" />
      <p className="mt-4 text-sm text-muted-foreground">{label}</p>
      <p className="mt-1 font-medium">{value}</p>
    </div>
  )
}
