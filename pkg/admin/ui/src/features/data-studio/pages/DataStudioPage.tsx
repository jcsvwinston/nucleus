import { useState, useEffect, useMemo } from 'react'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { useToast } from '@/components/ui/use-toast'
import * as api from '@/services/api'
import type { ModelSummary, ModelSchema, RuntimeInfo } from '@/types'
import ModelSidebar from '../components/ModelSidebar'
import RecordTable from '../components/RecordTable'
import { Database, Loader2, Server } from 'lucide-react'

export default function DataStudioPage() {
  const { toast } = useToast()

  const [models, setModels] = useState<ModelSummary[]>([])
  const [runtime, setRuntime] = useState<RuntimeInfo | null>(null)
  const [loadingModels, setLoadingModels] = useState(true)

  const [selectedModel, setSelectedModel] = useState<string | null>(null)
  const [schema, setSchema] = useState<ModelSchema | null>(null)
  const [loadingSchema, setLoadingSchema] = useState(false)
  const [dbAlias, setDbAlias] = useState<string | undefined>(undefined)

  // Load models on mount
  useEffect(() => {
    let cancelled = false
    const load = async () => {
      setLoadingModels(true)
      try {
        const res = await api.getModelsWithRuntime(true)
        if (cancelled) return
        setModels(res.models ?? [])
        setRuntime(res.runtime ?? null)
      } catch (err: any) {
        toast({ variant: 'destructive', title: 'Failed to load models', description: err.message })
      } finally {
        if (!cancelled) setLoadingModels(false)
      }
    }
    load()
    return () => { cancelled = true }
  }, [])

  // Load schema when model is selected
  useEffect(() => {
    if (!selectedModel) { setSchema(null); return }
    let cancelled = false
    const load = async () => {
      setLoadingSchema(true)
      try {
        const s = await api.getModelSchema(selectedModel)
        if (!cancelled) setSchema(s)
      } catch (err: any) {
        toast({ variant: 'destructive', title: 'Failed to load schema', description: err.message })
        if (!cancelled) setSchema(null)
      } finally {
        if (!cancelled) setLoadingSchema(false)
      }
    }
    load()
    return () => { cancelled = true }
  }, [selectedModel])

  // Databases the selected model lives on
  const modelDbs = useMemo(() => {
    if (!selectedModel || !runtime?.databases) return []
    const summary = models.find((m) => m.name === selectedModel)
    if (!summary?.databases?.length) return []
    return summary.databases.map((alias) => {
      const db = runtime.databases.find((d) => d.alias === alias)
      return {
        alias,
        engine: db?.dialect || db?.engine || 'unknown',
        isDefault: db?.is_default ?? false,
        count: summary.counts?.[alias] ?? -1,
        countKnown: summary.count_known && summary.counts?.[alias] !== undefined,
      }
    })
  }, [selectedModel, models, runtime])

  const handleSelectModel = (name: string, alias?: string) => {
    setSelectedModel(name)
    setDbAlias(alias)
  }

  return (
    <div className="flex flex-col h-[calc(100vh-4rem)]">
      {/* Header */}
      <div className="flex items-center justify-between pb-4">
        <div>
          <h1 className="text-2xl font-bold">Data Studio</h1>
          <p className="text-sm text-muted-foreground">
            Browse, search and manage your application data
          </p>
        </div>
      </div>

      {/* Main layout */}
      <div className="flex flex-1 min-h-0 gap-4">
        {/* Sidebar */}
        <div className="w-64 flex-shrink-0 border rounded-lg bg-card overflow-hidden">
          {loadingModels ? (
            <div className="flex items-center justify-center h-full">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          ) : (
            <ModelSidebar
              models={models}
              runtime={runtime}
              selectedModel={selectedModel}
              selectedDbAlias={dbAlias}
              onSelectModel={handleSelectModel}
            />
          )}
        </div>

        {/* Content area */}
        <div className="flex-1 min-w-0 border rounded-lg bg-card p-4 overflow-hidden flex flex-col">
          {!selectedModel ? (
            <div className="flex flex-col items-center justify-center h-full text-muted-foreground gap-3">
              <Database className="h-12 w-12 opacity-30" />
              <p className="text-sm">Select a model from the sidebar to browse its records</p>
              {models.length > 0 && (
                <p className="text-xs">
                  {models.length} model{models.length !== 1 ? 's' : ''} registered
                  {runtime?.databases && runtime.databases.length > 1
                    ? ` across ${runtime.databases.length} databases (${runtime.engines.join(', ')})`
                    : runtime?.engines?.length
                      ? ` on ${runtime.engines.join(', ')}`
                      : ''}
                </p>
              )}
            </div>
          ) : loadingSchema ? (
            <div className="flex items-center justify-center h-full">
              <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
            </div>
          ) : schema ? (
            <>
              {/* Model header + database selector */}
              <div className="flex items-start justify-between gap-4 pb-3 mb-0">
                <div className="flex items-center gap-2">
                  {schema.icon && <span className="text-xl">{schema.icon}</span>}
                  <div>
                    <h2 className="text-lg font-semibold leading-tight">{schema.plural || schema.name}</h2>
                    <p className="text-xs text-muted-foreground">
                      {schema.table}
                      {schema.read_only && ' (read-only)'}
                    </p>
                  </div>
                </div>

                {/* Multi-database selector */}
                {modelDbs.length > 1 && (
                  <div className="flex items-center gap-1.5 flex-shrink-0">
                    <Server className="h-3.5 w-3.5 text-muted-foreground" />
                    <div className="flex gap-1">
                      {modelDbs.map((db) => {
                        const isActive = dbAlias === db.alias || (!dbAlias && db.isDefault)
                        return (
                          <Button
                            key={db.alias}
                            variant={isActive ? 'default' : 'outline'}
                            size="sm"
                            className="h-7 text-xs gap-1.5"
                            onClick={() => setDbAlias(db.alias)}
                          >
                            {db.alias}
                            <span className="opacity-70 text-[10px]">{db.engine}</span>
                            {db.countKnown && (
                              <Badge variant={isActive ? 'secondary' : 'outline'} className="text-[9px] px-1 py-0">
                                {db.count.toLocaleString()}
                              </Badge>
                            )}
                          </Button>
                        )
                      })}
                    </div>
                  </div>
                )}

                {/* Single-database indicator */}
                {modelDbs.length === 1 && (
                  <span className="flex items-center gap-1 text-xs text-muted-foreground flex-shrink-0">
                    <Server className="h-3 w-3" />
                    {modelDbs[0].alias}
                    <span className="opacity-60">({modelDbs[0].engine})</span>
                  </span>
                )}
              </div>

              <RecordTable
                modelName={selectedModel}
                schema={schema}
                dbAlias={dbAlias}
              />
            </>
          ) : (
            <div className="flex items-center justify-center h-full text-muted-foreground">
              <p className="text-sm">Failed to load schema for {selectedModel}</p>
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
