import { useState, useMemo } from 'react'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import type { ModelSummary, RuntimeInfo } from '@/types'
import { Search, ChevronDown, ChevronRight, Server, Table2, Box } from 'lucide-react'

interface Props {
  models: ModelSummary[]
  runtime: RuntimeInfo | null
  selectedModel: string | null
  selectedDbAlias: string | undefined
  onSelectModel: (name: string, dbAlias?: string) => void
}

type ViewMode = 'all' | 'engine' | 'database'

/** Map known icon strings to lucide icons or emoji */
const ICON_MAP: Record<string, string> = {
  document: '\u{1F4C4}', file: '\u{1F4C4}', article: '\u{1F4F0}',
  user: '\u{1F464}', users: '\u{1F465}', person: '\u{1F464}',
  settings: '\u{2699}\u{FE0F}', config: '\u{2699}\u{FE0F}',
  mail: '\u{2709}\u{FE0F}', email: '\u{2709}\u{FE0F}',
  cart: '\u{1F6D2}', order: '\u{1F4E6}', product: '\u{1F4E6}',
  tag: '\u{1F3F7}\u{FE0F}', category: '\u{1F4C1}',
  image: '\u{1F5BC}\u{FE0F}', photo: '\u{1F4F7}',
  comment: '\u{1F4AC}', message: '\u{1F4AC}',
  star: '\u{2B50}', heart: '\u{2764}\u{FE0F}',
  lock: '\u{1F512}', key: '\u{1F511}',
  calendar: '\u{1F4C5}', clock: '\u{1F552}',
  home: '\u{1F3E0}', globe: '\u{1F310}',
}

function resolveIcon(raw: string): string | null {
  if (!raw) return null
  // Already an emoji (has codepoint > 255)
  if ([...raw].some((ch) => ch.codePointAt(0)! > 255)) return raw
  const mapped = ICON_MAP[raw.toLowerCase().trim()]
  return mapped ?? null
}

/** Get all databases a model lives on, with engine info */
function modelDatabases(model: ModelSummary, runtime: RuntimeInfo | null) {
  if (!model.databases?.length || !runtime?.databases) return []
  return model.databases.map((alias) => {
    const db = runtime.databases.find((d) => d.alias === alias)
    return {
      alias,
      engine: db?.dialect || db?.engine || 'unknown',
      isDefault: db?.is_default ?? false,
      count: model.counts?.[alias] ?? -1,
      countKnown: model.count_known && model.counts?.[alias] !== undefined,
    }
  })
}

export default function ModelSidebar({ models, runtime, selectedModel, selectedDbAlias, onSelectModel }: Props) {
  const [search, setSearch] = useState('')
  const [viewMode, setViewMode] = useState<ViewMode>('all')
  const [expandedGroups, setExpandedGroups] = useState<Set<string>>(new Set())
  const [dbFilter, setDbFilter] = useState<string | null>(null)

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
      list = list.filter((m) => m.databases?.includes(dbFilter))
    }
    return list
  }, [models, search, dbFilter])

  const engineGroups = useMemo(() => {
    if (!runtime?.engine_groups) return []
    return runtime.engine_groups.map((eg) => ({
      ...eg,
      models: filtered.filter((m) =>
        eg.databases.some((db) => m.databases?.includes(db.alias)),
      ),
    })).filter((eg) => eg.models.length > 0)
  }, [runtime, filtered])

  const databaseGroups = useMemo(() => {
    if (!runtime?.databases) return []
    return runtime.databases.map((db) => ({
      ...db,
      models: filtered.filter((m) => m.databases?.includes(db.alias)),
    })).filter((g) => g.models.length > 0)
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

  const renderModelIcon = (m: ModelSummary, isActive: boolean) => {
    const emoji = resolveIcon(m.icon)
    if (emoji) return <span className="text-base flex-shrink-0">{emoji}</span>
    return <Table2 className={`h-3.5 w-3.5 flex-shrink-0 ${isActive ? 'opacity-80' : 'opacity-40'}`} />
  }

  const renderModelItem = (m: ModelSummary, contextDbAlias?: string) => {
    const isActive = selectedModel === m.name && (contextDbAlias === undefined ? true : selectedDbAlias === contextDbAlias)
    const dbs = modelDatabases(m, runtime)
    const showDbBadges = multiDb && viewMode === 'all' && dbs.length > 0

    return (
      <button
        key={`${m.name}-${contextDbAlias ?? 'all'}`}
        onClick={() => onSelectModel(m.name, contextDbAlias)}
        className={`w-full text-left px-3 py-2 rounded-md text-sm transition-colors ${
          isActive
            ? 'bg-primary text-primary-foreground'
            : 'hover:bg-muted text-foreground'
        }`}
      >
        <span className="flex items-center justify-between gap-2">
          <span className="flex items-center gap-2 min-w-0">
            {renderModelIcon(m, isActive)}
            <span className="truncate font-medium">{m.plural || m.name}</span>
          </span>
          <span className="flex items-center gap-1.5 flex-shrink-0">
            {contextDbAlias && m.counts?.[contextDbAlias] !== undefined ? (
              <Badge variant={isActive ? 'secondary' : 'outline'} className="text-[10px] tabular-nums">
                {m.counts[contextDbAlias].toLocaleString()}
              </Badge>
            ) : m.count_known ? (
              <Badge variant={isActive ? 'secondary' : 'outline'} className="text-[10px] tabular-nums">
                {m.count.toLocaleString()}
              </Badge>
            ) : null}
          </span>
        </span>
        {showDbBadges && (
          <span className="flex items-center gap-1 mt-1 ml-5">
            {dbs.map((db) => (
              <span
                key={db.alias}
                className={`inline-flex items-center gap-0.5 px-1.5 py-0 rounded text-[9px] leading-4 ${
                  isActive ? 'bg-primary-foreground/20 text-primary-foreground' : 'bg-muted text-muted-foreground'
                }`}
              >
                <Server className="h-2 w-2" />
                {db.alias}
              </span>
            ))}
          </span>
        )}
        {!showDbBadges && (
          <span className={`block text-[10px] ml-5 mt-0.5 ${isActive ? 'text-primary-foreground/70' : 'text-muted-foreground'}`}>
            {m.table}
          </span>
        )}
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
          <div className="flex gap-1 flex-wrap">
            <button
              onClick={() => switchViewMode('all')}
              className={`px-2 py-0.5 rounded text-xs transition-colors ${
                viewMode === 'all' ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:text-foreground'
              }`}
            >
              All
            </button>
            {multiEngine && (
              <button
                onClick={() => switchViewMode('engine')}
                className={`px-2 py-0.5 rounded text-xs transition-colors ${
                  viewMode === 'engine' ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:text-foreground'
                }`}
              >
                By Engine
              </button>
            )}
            {multiDb && (
              <button
                onClick={() => switchViewMode('database')}
                className={`px-2 py-0.5 rounded text-xs transition-colors ${
                  viewMode === 'database' ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:text-foreground'
                }`}
              >
                By Database
              </button>
            )}
          </div>
        )}

        {viewMode === 'all' && multiDb && runtime?.databases && (
          <div className="flex flex-wrap gap-1">
            <button
              onClick={() => setDbFilter(null)}
              className={`px-1.5 py-0 rounded text-[10px] leading-5 transition-colors ${
                !dbFilter ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:text-foreground'
              }`}
            >
              All DBs
            </button>
            {runtime.databases.map((db) => (
              <button
                key={db.alias}
                onClick={() => setDbFilter(dbFilter === db.alias ? null : db.alias)}
                className={`px-1.5 py-0 rounded text-[10px] leading-5 transition-colors ${
                  dbFilter === db.alias ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:text-foreground'
                }`}
              >
                {db.alias}
              </button>
            ))}
          </div>
        )}
      </div>

      <div className="flex-1 overflow-y-auto p-2 space-y-0.5">
        {viewMode === 'all' && (
          filtered.length === 0 ? (
            <p className="text-xs text-muted-foreground text-center py-6">
              {search || dbFilter ? 'No models match your filter' : 'No models registered'}
            </p>
          ) : filtered.map((m) => renderModelItem(m))
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
          {runtime.counts_available && runtime.records_total >= 0 && (
            <div className="flex justify-between">
              <span>Records</span>
              <span>{runtime.records_total.toLocaleString()}</span>
            </div>
          )}
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
