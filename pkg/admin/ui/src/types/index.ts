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
