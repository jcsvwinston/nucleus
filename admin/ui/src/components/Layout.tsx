import { type ReactNode } from 'react'

export interface NavItem {
  id: string
  label: string
  badge?: number
}

export interface LayoutProps {
  current: string
  items: NavItem[]
  onNavigate: (id: string) => void
  serverHealthy: boolean
  children: ReactNode
}

export function Layout(props: LayoutProps) {
  return (
    <div className="flex min-h-screen bg-zinc-950 text-zinc-100">
      <aside className="w-56 shrink-0 border-r border-zinc-800 bg-zinc-900/40">
        <div className="px-5 py-5">
          <div className="text-xs font-semibold uppercase tracking-wider text-zinc-500">
            Nucleus
          </div>
          <div className="text-sm font-semibold">Observability</div>
        </div>
        <nav className="px-2">
          {props.items.map((item) => {
            const active = item.id === props.current
            return (
              <button
                key={item.id}
                type="button"
                onClick={() => props.onNavigate(item.id)}
                className={[
                  'flex w-full items-center justify-between rounded-md px-3 py-2 text-left text-sm transition-colors',
                  active
                    ? 'bg-zinc-800 text-zinc-100'
                    : 'text-zinc-400 hover:bg-zinc-800/60 hover:text-zinc-100',
                ].join(' ')}
              >
                <span>{item.label}</span>
                {item.badge !== undefined && (
                  <span
                    className={[
                      'rounded-full px-2 py-0.5 text-xs',
                      active ? 'bg-zinc-700' : 'bg-zinc-800',
                    ].join(' ')}
                  >
                    {item.badge}
                  </span>
                )}
              </button>
            )
          })}
        </nav>
        <div className="mt-auto px-5 py-4 text-xs text-zinc-500">
          <div className="flex items-center gap-2">
            <span
              className={[
                'h-2 w-2 rounded-full',
                props.serverHealthy ? 'bg-emerald-500' : 'bg-rose-500',
              ].join(' ')}
            />
            <span>{props.serverHealthy ? 'Server reachable' : 'Server unreachable'}</span>
          </div>
        </div>
      </aside>
      <main className="flex-1 overflow-x-hidden">{props.children}</main>
    </div>
  )
}

export function PageHeader(props: { title: string; subtitle?: string; actions?: ReactNode }) {
  return (
    <header className="flex items-center justify-between border-b border-zinc-800 px-8 py-5">
      <div>
        <h1 className="text-lg font-semibold">{props.title}</h1>
        {props.subtitle !== undefined && (
          <p className="text-sm text-zinc-500">{props.subtitle}</p>
        )}
      </div>
      {props.actions !== undefined && <div className="flex gap-2">{props.actions}</div>}
    </header>
  )
}

export function PageBody(props: { children: ReactNode }) {
  return <div className="p-8">{props.children}</div>
}
