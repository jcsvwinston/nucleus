import type { User, Session, Model, Record as AppRecord, AuditLog, RBACPolicy, HealthCheck, SystemMetrics, LiveRequest, FeatureFlag, ModelsResponse, ModelSchema, PaginatedResult, SystemSnapshot } from '@/types'
import { buildAdminPath } from '@/config'

function isRedirectToLogin(response: Response): boolean {
  const loginPath = buildAdminPath('/login')

  if (response.status === 401) {
    return true
  }

  if (!response.redirected || !response.url) {
    return false
  }

  try {
    const redirectedURL = new URL(response.url, window.location.origin)
    return redirectedURL.pathname === loginPath
  } catch {
    return false
  }
}

async function fetchAPI<T = unknown>(path: string, options?: RequestInit): Promise<T> {
  const url = buildAdminPath(path)

  const response = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...options?.headers,
    },
    credentials: 'same-origin',
  })

  if (isRedirectToLogin(response)) {
    window.location.href = buildAdminPath('/login')
    throw new Error('Unauthorized')
  }

  if (!response.ok) {
    const message = await response.text()
    throw new Error(message || `API Error: ${response.status} ${response.statusText}`)
  }

  const contentType = response.headers.get('content-type') ?? ''
  if (!contentType.includes('application/json')) {
    throw new Error(`Unexpected content type: ${contentType || 'unknown'}`)
  }

  return response.json() as Promise<T>
}

export async function login(username: string, password: string): Promise<User> {
  const formData = new URLSearchParams()
  formData.append('username', username)
  formData.append('password', password)

  const response = await fetch(buildAdminPath('/login'), {
    method: 'POST',
    body: formData,
    credentials: 'same-origin',
  })

  // 303 means success - browser will follow redirect
  if (response.ok || response.status === 303 || response.type === 'opaqueredirect') {
    const user: User = {
      id: 0,
      username,
      email: '',
      is_superuser: true,
    }
    return user
  }

  throw new Error(`Login failed: ${response.status} ${response.statusText}`)
}

export async function logout(): Promise<void> {
  await fetchAPI('/api/logout', { method: 'POST' })
  window.location.href = buildAdminPath('/login')
}

export async function getCurrentUser(): Promise<User | null> {
  try {
    // Use /api/models as auth check - returns 200 when authenticated.
    const response = await fetch(buildAdminPath('/api/models'), {
      credentials: 'same-origin',
    })
    if (isRedirectToLogin(response) || !response.ok) return null

    return {
      id: 0,
      username: 'admin',
      email: '',
      is_superuser: true,
    }
  } catch {
    return null
  }
}

export async function getModels(): Promise<Model[]> {
  const response = await fetchAPI<{
    models?: Array<{ name: string; table: string; count?: number }>
  }>('/api/models')

  return (response.models ?? []).map((model) => ({
    name: model.name,
    table: model.table,
    fields: [],
    count: model.count,
  }))
}

// ── Data Studio API ──

export async function getModelsWithRuntime(includeCounts = true): Promise<ModelsResponse> {
  const qs = includeCounts ? '?include_counts=true' : ''
  return fetchAPI<ModelsResponse>(`/api/models${qs}`)
}

export async function getModelSchema(name: string): Promise<ModelSchema> {
  return fetchAPI<ModelSchema>(`/api/models/${name}/schema`)
}

export interface FieldMetaUpdate {
  is_list?: boolean
  is_search?: boolean
  is_filter?: boolean
  is_excluded?: boolean
  is_readonly?: boolean
  label?: string
  html_type?: string
}

export async function updateFieldsMeta(
  modelName: string,
  fields: { [fieldName: string]: FieldMetaUpdate },
): Promise<void> {
  await fetchAPI(`/api/models/${modelName}/schema/fields`, {
    method: 'PUT',
    body: JSON.stringify({ fields }),
  })
}

export async function getRecordsPaginated(
  name: string,
  params: { page?: number; page_size?: number; search?: string; order_by?: string; db_alias?: string; filters?: Record<string, string> },
): Promise<PaginatedResult> {
  const searchParams = new URLSearchParams()
  if (params.page) searchParams.set('page', String(params.page))
  if (params.page_size) searchParams.set('page_size', String(params.page_size))
  if (params.search) searchParams.set('search', params.search)
  if (params.order_by) searchParams.set('order_by', params.order_by)
  if (params.db_alias) searchParams.set('db_alias', params.db_alias)
  if (params.filters) {
    for (const [k, v] of Object.entries(params.filters)) {
      searchParams.set(k, v)
    }
  }
  return fetchAPI<PaginatedResult>(`/api/models/${name}?${searchParams}`)
}

export async function getRecord(name: string, id: string): Promise<AppRecord> {
  return fetchAPI(`/api/models/${name}/${id}`)
}

export async function getRecords(name: string, params?: Record<string, string>): Promise<AppRecord[]> {
  const searchParams = new URLSearchParams(params)
  return fetchAPI(`/api/models/${name}?${searchParams}`)
}

export async function createRecord(name: string, data: AppRecord): Promise<AppRecord> {
  return fetchAPI(`/api/models/${name}`, {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateRecord(name: string, id: string, data: AppRecord): Promise<AppRecord> {
  return fetchAPI(`/api/models/${name}/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deleteRecord(name: string, id: string): Promise<void> {
  await fetchAPI(`/api/models/${name}/${id}`, { method: 'DELETE' })
}

export async function bulkDelete(name: string, ids: number[]): Promise<{ deleted: number; failed: number }> {
  return fetchAPI(`/api/models/${name}/bulk`, {
    method: 'POST',
    body: JSON.stringify({ action: 'delete', ids }),
  })
}

export async function getSessions(): Promise<Session[]> {
  const response = await fetchAPI<{
    sessions?: Array<{
      token: string
      user?: string
      remote_ip?: string
      first_seen_at?: string
      last_seen_at?: string
    }>
  }>('/api/sessions')

  return (response.sessions ?? []).map((session) => ({
    id: session.token,
    user_id: 0,
    username: session.user ?? '',
    ip: session.remote_ip ?? '',
    user_agent: '',
    created_at: session.first_seen_at ?? '',
    last_activity: session.last_seen_at ?? '',
  }))
}

export async function getAuditLogs(params?: Record<string, string>): Promise<AuditLog[]> {
  const searchParams = new URLSearchParams(params)
  const response = await fetchAPI<{
    entries?: Array<{
      id: number
      username?: string
      action: string
      model_name?: string
      record_id?: string
      created_at: string
    }>
  }>(`/api/audit?${searchParams}`)

  return (response.entries ?? []).map((entry) => ({
    id: entry.id,
    timestamp: entry.created_at,
    user: entry.username ?? '',
    action: entry.action,
    resource: [entry.model_name, entry.record_id].filter(Boolean).join('#'),
    details: entry.model_name ?? '',
  }))
}

export async function getRBACPolicies(): Promise<RBACPolicy[]> {
  const response = await fetchAPI<{
    policies?: Array<{ sub: string; obj: string; act: string }>
  }>('/api/rbac/policies')

  return (response.policies ?? []).map((policy) => ({
    ptype: 'p',
    v0: policy.sub,
    v1: policy.obj,
    v2: policy.act,
  }))
}

export async function createRBACPolicy(policy: Partial<RBACPolicy>): Promise<void> {
  await fetchAPI('/api/rbac/policies', {
    method: 'POST',
    body: JSON.stringify({
      sub: policy.v0,
      obj: policy.v1,
      act: policy.v2,
    }),
  })
}

export async function deleteRBACPolicy(policy: Partial<RBACPolicy>): Promise<void> {
  await fetchAPI('/api/rbac/policies', {
    method: 'DELETE',
    body: JSON.stringify({
      sub: policy.v0,
      obj: policy.v1,
      act: policy.v2,
    }),
  })
}

export async function getHealthChecks(): Promise<HealthCheck[]> {
  const response = await fetchAPI<{
    checks?: Array<{ name: string; status: 'healthy' | 'unhealthy' | 'unknown'; message?: string; latency_ms?: number }>
  }>('/api/health')

  return (response.checks ?? []).map((check) => ({
    name: check.name,
    status: check.status,
    latency: check.latency_ms,
    error: check.status === 'healthy' ? undefined : check.message,
  }))
}

export async function getSystemMetrics(): Promise<SystemMetrics> {
  const response = await getSystemSnapshot()

  return {
    goroutines: response.goroutines?.count ?? 0,
    memory: {
      alloc: response.memory?.alloc_bytes ?? 0,
      total_alloc: response.memory?.heap_alloc_bytes ?? 0,
      sys: response.memory?.heap_sys_bytes ?? 0,
      num_gc: response.memory?.num_gc ?? 0,
    },
    cpu_usage: response.process_cpu_load ?? response.cpu_load ?? 0,
    db_pools: (response.databases ?? []).map((database) => ({
      name: database.alias,
      open_connections: database.open_connections,
      in_use: database.in_use,
      idle: database.idle,
    })),
  }
}

export async function getSystemSnapshot(): Promise<SystemSnapshot> {
  return fetchAPI<SystemSnapshot>('/api/system/snapshot')
}

export async function getLiveRequests(): Promise<LiveRequest[]> {
  const response = await fetchAPI<{
    requests?: Array<{
      request_id?: string
      timestamp: string
      method: string
      path: string
      status: number
      duration_ms: number
    }>
  }>('/api/live/snapshot')

  return (response.requests ?? []).map((request) => ({
    id: request.request_id ?? `${request.timestamp}-${request.method}-${request.path}`,
    method: request.method,
    path: request.path,
    status: request.status,
    duration: request.duration_ms,
    timestamp: request.timestamp,
  }))
}

export function getLiveWebSocket(): WebSocket | null {
  try {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const path = buildAdminPath('/api/live/ws')
    return new WebSocket(`${protocol}//${window.location.host}${path}`)
  } catch {
    return null
  }
}

export async function getFeatureFlags(): Promise<FeatureFlag[]> {
  const response = await fetchAPI<{
    flags?: FeatureFlag[]
  }>('/api/features')
  return response.flags ?? []
}

export async function toggleFeatureFlag(name: string, enabled: boolean): Promise<void> {
  await fetchAPI(`/api/features/${name}`, {
    method: 'PUT',
    body: JSON.stringify({ enabled }),
  })
}

export async function exportData(format: 'csv' | 'json' | 'sql', modelName?: string): Promise<string> {
  const response = await fetchAPI<{ url?: string; storage_key?: string; id?: string }>('/api/export', {
    method: 'POST',
    body: JSON.stringify({
      format,
      models: modelName ? [modelName] : [],
    }),
  })
  // If the backend returns a full URL, use it directly
  if (response.url) return response.url
  // Otherwise construct a download URL from the storage key
  const key = response.storage_key || response.id
  if (key) {
    return buildAdminPath(`/api/export/download?key=${encodeURIComponent(key)}`)
  }
  return ''
}

export async function importData(file: File): Promise<void> {
  const formData = new FormData()
  formData.append('file', file)

  await fetch(buildAdminPath('/api/import/upload'), {
    method: 'POST',
    body: formData,
    credentials: 'same-origin',
  })
}
