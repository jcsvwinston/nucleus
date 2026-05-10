import { type ReactNode } from 'react'

export interface StreamControlsProps {
  connected: boolean
  paused: boolean
  onTogglePause: () => void
  onClear: () => void
  count: number
  error?: string | null
  extra?: ReactNode
}

export function StreamControls(props: StreamControlsProps) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <ConnectionPill connected={props.connected} />
      <button
        type="button"
        onClick={props.onTogglePause}
        className={[
          'rounded-md border px-3 py-1.5 text-sm transition-colors',
          props.paused
            ? 'border-amber-700 bg-amber-900/30 text-amber-300 hover:bg-amber-900/50'
            : 'border-zinc-700 bg-zinc-900 text-zinc-200 hover:bg-zinc-800',
        ].join(' ')}
      >
        {props.paused ? 'Resume' : 'Pause'}
      </button>
      <button
        type="button"
        onClick={props.onClear}
        className="rounded-md border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-sm text-zinc-200 hover:bg-zinc-800"
      >
        Clear
      </button>
      <span className="ml-2 text-xs text-zinc-500">
        {props.count.toLocaleString()} events buffered
      </span>
      {props.error !== undefined && props.error !== null && (
        <span className="ml-2 text-xs text-rose-400" title={props.error}>
          {props.error.slice(0, 80)}
        </span>
      )}
      {props.extra}
    </div>
  )
}

function ConnectionPill(props: { connected: boolean }) {
  return (
    <span
      className={[
        'inline-flex items-center gap-1.5 rounded-full px-2.5 py-0.5 text-xs',
        props.connected
          ? 'bg-emerald-900/40 text-emerald-300 ring-1 ring-emerald-700'
          : 'bg-zinc-800 text-zinc-400 ring-1 ring-zinc-700',
      ].join(' ')}
    >
      <span
        className={[
          'h-2 w-2 rounded-full',
          props.connected ? 'bg-emerald-400' : 'bg-zinc-600',
        ].join(' ')}
      />
      {props.connected ? 'Live' : 'Disconnected'}
    </span>
  )
}
