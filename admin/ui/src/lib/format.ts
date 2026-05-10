// Tiny set of formatters shared by the streaming pages. Kept in one
// place so changes (e.g. switching to a real i18n lib) hit one file.

import type { Duration, Timestamp } from '@bufbuild/protobuf'

export function timestampToDate(ts: Timestamp | undefined): Date | undefined {
  if (!ts) return undefined
  // Timestamp.seconds is bigint in protobuf-es 1.x.
  return new Date(Number(ts.seconds) * 1000 + Math.floor(ts.nanos / 1_000_000))
}

export function durationToMillis(d: Duration | undefined): number {
  if (!d) return 0
  return Number(d.seconds) * 1000 + d.nanos / 1_000_000
}

export function formatDuration(ms: number): string {
  if (ms < 1) return `${(ms * 1000).toFixed(0)}µs`
  if (ms < 1000) return `${ms.toFixed(2)}ms`
  return `${(ms / 1000).toFixed(2)}s`
}

export function formatRelative(date: Date | undefined): string {
  if (!date) return '—'
  const diff = Date.now() - date.getTime()
  if (diff < 1000) return 'just now'
  if (diff < 60_000) return `${Math.floor(diff / 1000)}s ago`
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`
  return date.toISOString().slice(0, 19).replace('T', ' ') + ' UTC'
}

export function formatTime(date: Date | undefined): string {
  if (!date) return '—'
  const h = date.getHours().toString().padStart(2, '0')
  const m = date.getMinutes().toString().padStart(2, '0')
  const s = date.getSeconds().toString().padStart(2, '0')
  const ms = date.getMilliseconds().toString().padStart(3, '0')
  return `${h}:${m}:${s}.${ms}`
}

export function statusClass(status: number): string {
  if (status === 0) return 'text-zinc-500'
  if (status < 300) return 'text-emerald-400'
  if (status < 400) return 'text-sky-400'
  if (status < 500) return 'text-amber-400'
  return 'text-rose-400'
}

export function methodClass(method: string): string {
  switch (method.toUpperCase()) {
    case 'GET':
      return 'text-sky-400'
    case 'POST':
      return 'text-emerald-400'
    case 'PUT':
    case 'PATCH':
      return 'text-amber-400'
    case 'DELETE':
      return 'text-rose-400'
    default:
      return 'text-zinc-400'
  }
}
