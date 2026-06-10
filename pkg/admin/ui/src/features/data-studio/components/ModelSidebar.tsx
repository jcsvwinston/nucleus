import { useState, useMemo } from 'react'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import type { ModelSummary, RuntimeInfo } from '@/types'
import { Search, ChevronDown, ChevronRight, Table2, Box, Database, Filter } from 'lucide-react'

interface Props {
  models: ModelSummary[]
  runtime: RuntimeInfo | null
  selectedModel: string | null
  selectedDbAlias: string | undefined
  onSelectModel: (name: string, dbAlias?: string) => void
}

type ViewMode = 'all' | 'engine' | 'database'

export default function ModelSidebar({ models, runtime, selectedModel, selectedDbAlias, onSelectModel }: Props) {
  const [search, setSearch] = useState('')
  const [viewMode, setViewMode] = useState<ViewMode>('all')
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set())
  const [dbFilter, setDbFilter] = useState<string | null>(null)
  const [engineFilter, setEngineFilter] = useState<string | null>(null)

  const multiEngine = (runtime?.engines?.length ?? 0) > 1
  const multiDb = (runtime?.databases?.length ?? 0) > 1

  const filtered = useMemo(() => {
    let list = models
    if (search.trim()) {
      const q = search.toLowerCase()
      list = list.filter(
        (m) =>
          m.name.toLowerCase().includes(q) ||
          m.table.toLowerCase().includes(q) ||
          (m.plural && m.plural.toLowerCase().includes(q)),
      )
    }
    if (dbFilter) {
      // Match against the probed homes (where the table actually exists),
      // falling back to the declared alias — same rule as databaseGroups.
      list = list.filter((m) => {
        const homes = m.databases && m.databases.length > 0 ? m.databases : [m.database]
        return homes.includes(dbFilter)
      })
    }
    if (engineFilter) {
      list = list.filter((m) => m.engine === engineFilter)
    }
    return list
  }, [models, search, dbFilter, engineFilter])

  const engineGroups = useMemo(() => {
    if (!runtime?.engine_groups) return []
    const groups = new Map<string, ModelSummary[]>()
    
    filtered.forEach(m => {
      const group = groups.get(m.engine) || []
      group.push(m)
      groups.set(m.engine, group)
    })

    return runtime.engine_groups.map((eg) => ({
      ...eg,
      models: groups.get(eg.name) || [],
    })).filter((eg) => eg.models.length > 0)
  }, [runtime, filtered])

  const databaseGroups = useMemo(() => {
    if (!runtime?.databases) return []
    const groups = new Map<string, ModelSummary[]>()

    // Group each model under EVERY database that actually holds its table
    // (the probed `databases` array from the API) — not just the declared
    // alias. In tenant-isolated topologies a model's data lives in several
    // tenant databases while the declared alias stays "default".
    filtered.forEach(m => {
      const homes = m.databases && m.databases.length > 0 ? m.databases : [m.database]
      homes.forEach(alias => {
        const group = groups.get(alias) || []
        group.push(m)
        groups.set(alias, group)
      })
    })

    // Keep every configured database visible (an empty one still exists and
    // is selectable as a browsing target).
    return runtime.databases.map((db) => ({
      ...db,
      models: groups.get(db.alias) || [],
    }))
  }, [runtime, filtered])

  const toggleGroup = (name: string) => {
    setExpandedGroups((prev) => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  const switchViewMode = (mode: ViewMode) => {
    setViewMode(mode)
    if (mode === 'engine' && runtime?.engines) {
      setExpandedGroups(new Set(runtime.engines))
    } else if (mode === 'database' && runtime?.databases) {
      setExpandedGroups(new Set(runtime.databases.map((d) => d.alias)))
    }
  }

  const renderModelItem = (m: ModelSummary, contextDbAlias?: string) => {
    const isActive = selectedModel === m.name && (contextDbAlias === undefined ? true : selectedDbAlias === contextDbAlias)

    return (
      <button
        key={`${m.name}-${contextDbAlias ?? 'all'}`}
        onClick={() => onSelectModel(m.name, contextDbAlias)}
        className={`w-full text-left px-3 py-2 rounded-md text-sm transition-all duration-200 ${
          isActive
            ? 'bg-primary text-primary-foreground shadow-md transform scale-[1.02]'
            : 'hover:bg-muted text-foreground hover:translate-x-1'
        }`}
      >
        <div className="flex items-center justify-between gap-2 min-w-0">
          <div className="flex items-center gap-2 truncate">
            <Table2 className={`h-3.5 w-3.5 flex-shrink-0 ${isActive ? 'opacity-80' : 'opacity-40'}`} />
            <span className="truncate font-medium">{m.plural || m.name}</span>
          </div>
          {m.count_known && (
            <Badge 
              variant={isActive ? 'secondary' : 'outline'} 
              className={`text-[10px] px-1 h-4 flex-shrink-0 font-normal ${isActive ? 'bg-primary-foreground/20 border-none text-primary-foreground' : 'text-muted-foreground'}`}
            >
              {m.count === -1 ? '?' : m.count.toLocaleString()}
              {m.is_estimated && <span className="ml-0.5 opacity-60">~</span>}
            </Badge>
          )}
        </div>
        <div className={`block text-[10px] ml-5.5 mt-0.5 ${isActive ? 'text-primary-foreground/70' : 'text-muted-foreground'}`}>
          {m.table}
        </div>
      </button>
    )
  }

  const renderGroupSection = (
    key: string,
    label: string,
    subtitle: string,
    items: ModelSummary[],
    contextDbAlias?: string,
  ) => {
    const isExpanded = expandedGroups.has(key)
    return (
      <div key={key}>
        <button
          onClick={() => toggleGroup(key)}
          className="w-full flex items-center gap-1.5 px-2 py-1.5 text-xs font-medium text-muted-foreground hover:text-foreground"
        >
          {isExpanded ? <ChevronDown className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
          <Box className="h-3 w-3" />
          <span className="truncate">{label}</span>
          {subtitle && <span className="text-[10px] opacity-60 truncate">{subtitle}</span>}
          <Badge variant="outline" className="text-[10px] ml-auto flex-shrink-0">
            {items.length}
          </Badge>
        </button>
        {isExpanded && (
          <div className="ml-3 space-y-0.5">
            {items.map((m) => renderModelItem(m, contextDbAlias))}
          </div>
        )}
      </div>
    )
  }

  return (
    <div className="flex flex-col h-full">
      <div className="p-3 border-b space-y-2">
        <div className="relative">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Filter models..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="pl-8 h-9"
          />
        </div>

        {(multiEngine || multiDb) && (
          <div className="space-y-3 pt-1">
            <div className="flex items-center justify-between">
              <span className="text-[10px] font-bold uppercase tracking-widest text-muted-foreground flex items-center gap-1.5">
                <Filter className="h-3 w-3" />
                View & Filter
              </span>
              <div className="flex bg-muted/50 p-0.5 rounded-lg border">
                <button
                  onClick={() => switchViewMode('all')}
                  className={`px-2 py-0.5 rounded-md text-[10px] font-medium transition-all ${
                    viewMode === 'all' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'
                  }`}
                >
                  All
                </button>
                {multiEngine && (
                  <button
                    onClick={() => switchViewMode('engine')}
                    className={`px-2 py-0.5 rounded-md text-[10px] font-medium transition-all ${
                      viewMode === 'engine' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    Engine
                  </button>
                )}
                {multiDb && (
                  <button
                    onClick={() => switchViewMode('database')}
                    className={`px-2 py-0.5 rounded-md text-[10px] font-medium transition-all ${
                      viewMode === 'database' ? 'bg-background text-foreground shadow-sm' : 'text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    DB
                  </button>
                )}
              </div>
            </div>

            <div className="grid grid-cols-1 gap-2">
              {multiEngine && runtime?.engines && (
                <div className="space-y-1">
                  <label className="text-[9px] font-semibold text-muted-foreground flex items-center gap-1">
                    <Database className="h-2.5 w-2.5" />
                    ENGINE FILTER
                  </label>
                  <div className="flex flex-wrap gap-1">
                    <button
                      onClick={() => setEngineFilter(null)}
                      className={`px-2 py-0.5 rounded-full text-[10px] border transition-all ${
                        !engineFilter ? 'bg-primary border-primary text-primary-foreground font-semibold' : 'bg-background hover:border-muted-foreground text-muted-foreground'
                      }`}
                    >
                      All
                    </button>
                    {runtime.engines.map((eng) => (
                      <button
                        key={eng}
                        onClick={() => setEngineFilter(engineFilter === eng ? null : eng)}
                        className={`px-2 py-0.5 rounded-full text-[10px] border transition-all ${
                          engineFilter === eng ? 'bg-primary border-primary text-primary-foreground font-semibold' : 'bg-background hover:border-muted-foreground text-muted-foreground'
                        }`}
                      >
                        {eng.toUpperCase()}
                      </button>
                    ))}
                  </div>
                </div>
              )}

              {multiDb && runtime?.databases && (
                <div className="space-y-1">
                  <label className="text-[9px] font-semibold text-muted-foreground flex items-center gap-1">
                    <Box className="h-2.5 w-2.5" />
                    DATABASE FILTER
                  </label>
                  <select
                    value={dbFilter ?? ''}
                    onChange={(e) => setDbFilter(e.target.value || null)}
                    className="w-full h-8 bg-muted/30 border rounded-md px-2 text-[10px] font-medium focus:outline-none focus:ring-1 focus:ring-primary appearance-none cursor-pointer hover:bg-muted/50 transition-colors"
                  >
                    <option value="">All Databases</option>
                    {runtime.databases
                      .filter(db => !engineFilter || (db.dialect || db.engine) === engineFilter)
                      .map((db) => (
                        <option key={db.alias} value={db.alias}>
                          {db.alias} ({db.dialect || db.engine})
                        </option>
                      ))}
                  </select>
                </div>
              )}
            </div>
          </div>
        )}
      </div>

      <div className="flex-1 overflow-y-auto p-2 space-y-0.5">
        {viewMode === 'all' && (
          filtered.length === 0 ? (
            <p className="text-xs text-muted-foreground text-center py-6">
              {search || dbFilter ? 'No models match your filter' : 'No models registered'}
            </p>
          ) : filtered.map((m) => renderModelItem(m, dbFilter ?? undefined)) /* null → undefined keeps the unfiltered isActive short-circuit */
        )}

        {viewMode === 'engine' && (
          engineGroups.length === 0 ? (
            <p className="text-xs text-muted-foreground text-center py-6">No engines available</p>
          ) : engineGroups.map((eg) =>
            renderGroupSection(eg.name, eg.name, `${eg.databases.length} db`, eg.models),
          )
        )}

        {viewMode === 'database' && (
          databaseGroups.length === 0 ? (
            <p className="text-xs text-muted-foreground text-center py-6">No databases available</p>
          ) : databaseGroups.map((db) =>
            renderGroupSection(db.alias, db.alias, db.dialect || db.engine || '', db.models, db.alias),
          )
        )}
      </div>

      {runtime && (
        <div className="border-t px-3 py-2 text-[11px] text-muted-foreground space-y-0.5">
          <div className="flex justify-between">
            <span>Models</span>
            <span>{runtime.models_total}</span>
          </div>
          {runtime.databases.length > 0 && (
            <div className="flex justify-between">
              <span>Databases</span>
              <span>{runtime.databases.length} ({runtime.engines.join(', ')})</span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
