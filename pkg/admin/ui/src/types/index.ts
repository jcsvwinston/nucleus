export interface User {
  id: number
  username: string
  email: string
  is_superuser: boolean
}

export interface Session {
  id: string
  user_id: number
  username: string
  ip: string
  user_agent: string
  created_at: string
  last_activity: string
}

// ── Data Studio types (match backend handleListModels / handleGetSchema) ──

export interface ModelSummary {
  name: string
  plural: string
  table: string
  icon: string
  count: number
  count_known: boolean
  counts?: { [alias: string]: number }
  databases?: string[]
}

export interface ModelSchema {
  name: string
  plural: string
  table: string
  primary_key: string
  icon: string
  read_only: boolean
  fields: SchemaField[]
  foreign_keys: ForeignKeyInfo[]
  tenant_field: string
}

export interface SchemaField {
  name: string
  column: string
  label: string
  type: string
  html_type: string
  is_pk: boolean
  is_required: boolean
  is_readonly: boolean
  is_list: boolean
  is_search: boolean
  is_filter: boolean
  is_excluded: boolean
  is_fk: boolean
  is_tenant_field: boolean
  fk_model?: string
  choices?: FieldChoice[]
}

export interface FieldChoice {
  value: string
  label: string
}

export interface ForeignKeyInfo {
  field_name: string
  column: string
  foreign_model: string
  foreign_table: string
  foreign_column: string
}

export interface PaginatedResult {
  items: { [key: string]: any }[]
  total: number
  page: number
  page_size: number
  total_pages: number
}

export interface RuntimeDatabaseInfo {
  alias: string
  engine: string
  dialect: string
  is_default: boolean
  models: string[]
  model_entries: { name: string; plural: string; table: string; count: number; count_known: boolean }[]
  model_count: number
}

export interface RuntimeEngineGroup {
  name: string
  databases: RuntimeDatabaseInfo[]
}

export interface RuntimeInfo {
  environment: string
  databases: RuntimeDatabaseInfo[]
  engines: string[]
  engine_groups: RuntimeEngineGroup[]
  trace_url_template?: string
  models_total: number
  records_total: number
  counts_mode: string
  counts_available: boolean
  sessions_active: number
  multi_tenant_enabled: boolean
  multi_tenant_default: string
  tenant_ids?: string[]
  multi_site_enabled: boolean
  multi_site_default: string
  site_names?: string[]
}

export interface ModelsResponse {
  models: ModelSummary[]
  title: string
  runtime: RuntimeInfo
}

// ── Legacy aliases for existing pages ──

export interface Model {
  name: string
  table: string
  fields: Field[]
  count?: number
}

export interface Field {
  name: string
  type: string
  primary: boolean
  nullable: boolean
}

export type Record = { [key: string]: any }

// ── Other page types ──

export interface AuditLog {
  id: number
  timestamp: string
  user: string
  action: string
  resource: string
  details: string
}

export interface RBACPolicy {
  ptype: string
  v0: string
  v1: string
  v2: string
}

export interface HealthCheck {
  name: string
  status: 'healthy' | 'unhealthy' | 'unknown'
  latency?: number
  error?: string
}

export interface SystemMetrics {
  goroutines: number
  memory: {
    alloc: number
    total_alloc: number
    sys: number
    num_gc: number
  }
  cpu_usage: number
  db_pools: {
    name: string
    open_connections: number
    in_use: number
    idle: number
  }[]
}

export interface RuntimeQueueSnapshot {
  name: string
  paused: boolean
  latency_ms: number
  size: number
  pending: number
  active: number
  scheduled: number
  retry: number
  archived: number
  completed: number
  aggregating: number
  processed_today: number
  failed_today: number
  processed_all: number
  failed_all: number
}

export interface RuntimeScheduleSnapshot {
  id: string
  spec: string
  task_type: string
  next_enqueue_at?: string
  prev_enqueue_at?: string
}

export interface TasksRuntimeSnapshot {
  enabled: boolean
  generated_at: string
  reason?: string
  queues: RuntimeQueueSnapshot[]
  schedules: RuntimeScheduleSnapshot[]
  total_schedules: number
  total_queues: number
  total_size: number
  total_pending: number
  total_active: number
  total_scheduled: number
  total_retry: number
  total_archived: number
  total_processed_today: number
  total_failed_today: number
}

export interface OutboxRuntimeSnapshot {
  enabled: boolean
  table: string
  flavor?: string
  reason?: string
  pending: number
  processing: number
  delivered: number
  failed: number
  total: number
  oldest_pending_at?: string
  last_delivered_at?: string
}

export interface LiveClusterSnapshot {
  enabled: boolean
  connected: boolean
  node_id?: string
  channel?: string
  reason?: string
  published: number
  dropped: number
  received: number
  ignored: number
}

export interface LiveNodeSnapshot {
  node_id: string
  last_seen_at?: string
  last_event_type?: string
  requests: number
  sql_queries: number
  sessions: number
  status: string
}

export interface SystemDatabasePool {
  alias: string
  engine: string
  dialect: string
  is_default: boolean
  open_connections: number
  in_use: number
  idle: number
  wait_count: number
  wait_duration_ms: number
  max_open_connections: number
  error?: string
}

export interface SystemSnapshot {
  enabled: boolean
  generated_at: string
  go_version: string
  go_os: string
  go_arch: string
  gomaxprocs: number
  cpus: number
  cpu_load: number
  process_cpu_load: number
  goroutines: {
    count: number
    state_counts: Array<{ state: string; count: number }>
  }
  memory: {
    alloc_bytes: number
    heap_alloc_bytes: number
    heap_sys_bytes: number
    stack_in_use_bytes: number
    heap_objects: number
    num_gc: number
    last_pause_ms: number
    pause_total_ms: number
  }
  databases: SystemDatabasePool[]
  jobs: TasksRuntimeSnapshot
  outbox: OutboxRuntimeSnapshot
  cluster: LiveClusterSnapshot
  cluster_nodes: LiveNodeSnapshot[]
  flags: FeatureFlag[]
  telemetry: {
    otlp_configured: boolean
    otlp_endpoint?: string
    trace_links_configured: boolean
    trace_url_template?: string
  }
  environment: Array<{
    name: string
    value: string
    masked: boolean
  }>
}

export interface LiveRequest {
  id: string
  method: string
  path: string
  status: number
  duration: number
  timestamp: string
}

export interface FeatureFlag {
  name: string
  enabled: boolean
}
