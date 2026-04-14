import { useState, useEffect, useCallback } from 'react'
import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from '@/components/ui/table'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { useToast } from '@/components/ui/use-toast'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from '@/components/ui/dialog'
import type { ModelSchema, SchemaField, PaginatedResult, Record as AppRecord } from '@/types'
import * as api from '@/services/api'
import RecordForm from './RecordForm'
import {
  Search, Plus, Pencil, Trash2, Loader2, ChevronLeft, ChevronRight,
  ChevronsLeft, ChevronsRight, ArrowUpDown, ArrowUp, ArrowDown,
  Download, Upload, FileText, X, Filter,
} from 'lucide-react'

interface Props {
  modelName: string
  schema: ModelSchema
  dbAlias?: string
}

type SortDir = 'asc' | 'desc' | null

const PAGE_SIZES = [10, 25, 50, 100]

/**
 * Read a field value from a record. The backend may return fields in
 * snake_case (column name) or PascalCase (Go field name), so we try both.
 */
function readField(row: AppRecord, field: SchemaField): any {
  if (field.column in row) return row[field.column]
  if (field.name in row) return row[field.name]
  // Try lowercase match as last resort
  const lc = field.column.toLowerCase()
  for (const key of Object.keys(row)) {
    if (key.toLowerCase() === lc) return row[key]
  }
  return undefined
}

function formatCellValue(value: any, field: SchemaField): string {
  if (value === null || value === undefined) return '—'
  if (field.html_type === 'checkbox') return value ? 'Yes' : 'No'
  if (field.html_type === 'datetime-local' || field.type === 'time.Time') {
    if (typeof value === 'string' && value.length > 0) {
      try {
        const d = new Date(value)
        return d.toLocaleString()
      } catch { return String(value) }
    }
  }
  const s = String(value)
  return s.length > 80 ? s.slice(0, 80) + '...' : s
}

/** Fix labels like "I D" → "ID" */
function fixLabel(label: string): string {
  if (/^[A-Z](\s[A-Z])+$/.test(label)) return label.replace(/\s/g, '')
  return label
}

function cellAlignClass(field: SchemaField): string {
  if (field.html_type === 'number') return 'text-right tabular-nums'
  if (field.html_type === 'checkbox') return 'text-center'
  return ''
}

export default function RecordTable({ modelName, schema, dbAlias }: Props) {
  const { toast } = useToast()

  // Data state
  const [result, setResult] = useState<PaginatedResult | null>(null)
  const [loading, setLoading] = useState(false)

  // Query state
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(25)
  const [search, setSearch] = useState('')
  const [searchInput, setSearchInput] = useState('')
  const [sortColumn, setSortColumn] = useState<string | null>(null)
  const [sortDir, setSortDir] = useState<SortDir>(null)
  const [activeFilters, setActiveFilters] = useState<Record<string, string>>({})
  const [showFilters, setShowFilters] = useState(false)

  // Selection state
  const [selected, setSelected] = useState<Set<number>>(new Set())

  // Dialog state
  const [formOpen, setFormOpen] = useState(false)
  const [editingRecord, setEditingRecord] = useState<AppRecord | null>(null)
  const [deleteId, setDeleteId] = useState<number | null>(null)
  const [deleting, setDeleting] = useState(false)

  // Export/Import state
  const [showExportImport, setShowExportImport] = useState(false)
  const [exportFormat, setExportFormat] = useState<'csv' | 'json' | 'sql'>('json')
  const [isExporting, setIsExporting] = useState(false)
  const [isImporting, setIsImporting] = useState(false)
  const [importFile, setImportFile] = useState<File | null>(null)

  const listFields = schema.fields.filter((f) => f.is_list && !f.is_excluded)
  const filterFields = schema.fields.filter((f) => f.is_filter && !f.is_excluded)
  const pkField = schema.fields.find((f) => f.is_pk)
  const pkColumn = pkField?.column ?? 'id'

  const fetchRecords = useCallback(async () => {
    setLoading(true)
    try {
      const orderBy = sortColumn && sortDir ? `${sortColumn} ${sortDir}` : undefined
      const cleanFilters: Record<string, string> = {}
      for (const [k, v] of Object.entries(activeFilters)) {
        if (v.trim()) cleanFilters[k] = v
      }
      const res = await api.getRecordsPaginated(modelName, {
        page,
        page_size: pageSize,
        search: search || undefined,
        order_by: orderBy,
        db_alias: dbAlias,
        filters: Object.keys(cleanFilters).length > 0 ? cleanFilters : undefined,
      })
      setResult(res)
    } catch (err: any) {
      toast({ variant: 'destructive', title: 'Failed to load records', description: err.message })
    } finally {
      setLoading(false)
    }
  }, [modelName, page, pageSize, search, sortColumn, sortDir, activeFilters, dbAlias])

  // Reset state when model changes
  useEffect(() => {
    setPage(1)
    setSearch('')
    setSearchInput('')
    setSortColumn(null)
    setSortDir(null)
    setActiveFilters({})
    setSelected(new Set())
    setResult(null)
  }, [modelName])

  useEffect(() => {
    fetchRecords()
  }, [fetchRecords])

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    setSearch(searchInput)
    setPage(1)
  }

  const clearSearch = () => {
    setSearchInput('')
    setSearch('')
    setPage(1)
  }

  const toggleSort = (column: string) => {
    if (sortColumn === column) {
      if (sortDir === 'asc') setSortDir('desc')
      else if (sortDir === 'desc') { setSortColumn(null); setSortDir(null) }
      else setSortDir('asc')
    } else {
      setSortColumn(column)
      setSortDir('asc')
    }
    setPage(1)
  }

  const toggleSelectAll = () => {
    if (!result?.items) return
    const ids = result.items.map((r) => (pkField ? readField(r, pkField) : r[pkColumn]) as number)
    if (selected.size === ids.length) setSelected(new Set())
    else setSelected(new Set(ids))
  }

  const toggleSelect = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const handleCreate = () => {
    setEditingRecord(null)
    setFormOpen(true)
  }

  const handleEdit = (record: AppRecord) => {
    setEditingRecord(record)
    setFormOpen(true)
  }

  const handleSave = async (data: AppRecord) => {
    if (editingRecord) {
      const id = String(pkField ? readField(editingRecord, pkField) : editingRecord[pkColumn])
      await api.updateRecord(modelName, id, data)
      toast({ title: 'Record updated' })
    } else {
      await api.createRecord(modelName, data)
      toast({ title: 'Record created' })
    }
    fetchRecords()
  }

  const confirmDelete = async () => {
    if (deleteId === null) return
    setDeleting(true)
    try {
      await api.deleteRecord(modelName, String(deleteId))
      toast({ title: 'Record deleted' })
      setDeleteId(null)
      fetchRecords()
    } catch (err: any) {
      toast({ variant: 'destructive', title: 'Delete failed', description: err.message })
    } finally {
      setDeleting(false)
    }
  }

  const handleBulkDelete = async () => {
    if (selected.size === 0) return
    try {
      const res = await api.bulkDelete(modelName, Array.from(selected))
      toast({ title: `Deleted ${res.deleted} record(s)${res.failed > 0 ? `, ${res.failed} failed` : ''}` })
      setSelected(new Set())
      fetchRecords()
    } catch (err: any) {
      toast({ variant: 'destructive', title: 'Bulk delete failed', description: err.message })
    }
  }

  const handleExport = async () => {
    setIsExporting(true)
    try {
      const url = await api.exportData(exportFormat, modelName)
      toast({ title: 'Export successful' })
      if (url) window.open(url, '_blank')
    } catch (err: any) {
      toast({ variant: 'destructive', title: 'Export failed', description: err.message })
    } finally {
      setIsExporting(false)
    }
  }

  const handleImport = async () => {
    if (!importFile) return
    setIsImporting(true)
    try {
      await api.importData(importFile)
      toast({ title: 'Import successful' })
      setImportFile(null)
      fetchRecords()
    } catch (err: any) {
      toast({ variant: 'destructive', title: 'Import failed', description: err.message })
    } finally {
      setIsImporting(false)
    }
  }

  const updateFilter = (column: string, value: string) => {
    setActiveFilters((prev) => ({ ...prev, [column]: value }))
    setPage(1)
  }

  const clearFilters = () => {
    setActiveFilters({})
    setPage(1)
  }

  const activeFilterCount = Object.values(activeFilters).filter((v) => v.trim()).length

  const items = result?.items ?? []
  const total = result?.total ?? 0
  const totalPages = result?.total_pages ?? 0

  return (
    <div className="flex flex-col h-full">
      {/* Toolbar */}
      <div className="flex flex-wrap items-center gap-2 pb-3 border-b">
        <form onSubmit={handleSearch} className="relative flex-1 min-w-[200px] max-w-sm">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search records..."
            value={searchInput}
            onChange={(e) => setSearchInput(e.target.value)}
            className="pl-8 pr-8 h-9"
          />
          {searchInput && (
            <button type="button" onClick={clearSearch} className="absolute right-2.5 top-2.5 text-muted-foreground hover:text-foreground">
              <X className="h-4 w-4" />
            </button>
          )}
        </form>

        {filterFields.length > 0 && (
          <Button variant="outline" size="sm" onClick={() => setShowFilters(!showFilters)} className="gap-1.5">
            <Filter className="h-3.5 w-3.5" />
            Filters
            {activeFilterCount > 0 && (
              <Badge variant="secondary" className="text-[10px] px-1.5">{activeFilterCount}</Badge>
            )}
          </Button>
        )}

        <div className="flex-1" />

        {selected.size > 0 && !schema.read_only && (
          <Button variant="destructive" size="sm" onClick={handleBulkDelete} className="gap-1.5">
            <Trash2 className="h-3.5 w-3.5" />
            Delete {selected.size}
          </Button>
        )}

        <Button variant="outline" size="sm" onClick={() => setShowExportImport(!showExportImport)} className="gap-1.5">
          <Download className="h-3.5 w-3.5" />
          Export / Import
        </Button>

        {!schema.read_only && (
          <Button size="sm" onClick={handleCreate} className="gap-1.5">
            <Plus className="h-3.5 w-3.5" />
            New {schema.name}
          </Button>
        )}
      </div>

      {/* Filter bar */}
      {showFilters && filterFields.length > 0 && (
        <div className="flex flex-wrap items-end gap-3 py-3 border-b">
          {filterFields.map((f) => {
            if (f.choices && f.choices.length > 0) {
              return (
                <div key={f.column} className="space-y-1">
                  <label className="text-xs text-muted-foreground">{f.label}</label>
                  <select
                    value={activeFilters[f.column] ?? ''}
                    onChange={(e) => updateFilter(f.column, e.target.value)}
                    className="flex h-8 rounded-md border border-input bg-background px-2 text-xs ring-offset-background focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  >
                    <option value="">All</option>
                    {f.choices.map((c) => (
                      <option key={c.value} value={c.value}>{c.label || c.value}</option>
                    ))}
                  </select>
                </div>
              )
            }
            if (f.html_type === 'checkbox') {
              return (
                <div key={f.column} className="space-y-1">
                  <label className="text-xs text-muted-foreground">{f.label}</label>
                  <select
                    value={activeFilters[f.column] ?? ''}
                    onChange={(e) => updateFilter(f.column, e.target.value)}
                    className="flex h-8 rounded-md border border-input bg-background px-2 text-xs ring-offset-background focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring"
                  >
                    <option value="">All</option>
                    <option value="1">Yes</option>
                    <option value="0">No</option>
                  </select>
                </div>
              )
            }
            return (
              <div key={f.column} className="space-y-1">
                <label className="text-xs text-muted-foreground">{f.label}</label>
                <Input
                  value={activeFilters[f.column] ?? ''}
                  onChange={(e) => updateFilter(f.column, e.target.value)}
                  placeholder={f.label}
                  className="h-8 text-xs w-32"
                />
              </div>
            )
          })}
          {activeFilterCount > 0 && (
            <Button variant="ghost" size="sm" onClick={clearFilters} className="text-xs h-8">
              Clear all
            </Button>
          )}
        </div>
      )}

      {/* Export/Import panel */}
      {showExportImport && (
        <div className="flex flex-wrap items-end gap-4 py-3 border-b">
          <div className="flex items-end gap-2">
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">Format</label>
              <div className="flex gap-1">
                {(['csv', 'json', 'sql'] as const).map((fmt) => (
                  <button
                    key={fmt}
                    onClick={() => setExportFormat(fmt)}
                    className={`px-2 py-1 rounded text-xs transition-colors ${
                      exportFormat === fmt ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground hover:text-foreground'
                    }`}
                  >
                    {fmt.toUpperCase()}
                  </button>
                ))}
              </div>
            </div>
            <Button size="sm" variant="outline" onClick={handleExport} disabled={isExporting} className="gap-1.5 h-8">
              {isExporting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Download className="h-3.5 w-3.5" />}
              Export
            </Button>
          </div>
          <div className="h-6 w-px bg-border" />
          <div className="flex items-end gap-2">
            <div className="space-y-1">
              <label className="text-xs text-muted-foreground">Import file</label>
              <label className="flex items-center gap-1.5 px-2 py-1 rounded border border-dashed text-xs cursor-pointer hover:bg-muted h-8">
                <FileText className="h-3.5 w-3.5" />
                {importFile ? importFile.name : 'Choose file...'}
                <input type="file" accept=".csv,.json,.sql" onChange={(e) => setImportFile(e.target.files?.[0] || null)} className="hidden" />
              </label>
            </div>
            <Button size="sm" variant="outline" onClick={handleImport} disabled={isImporting || !importFile} className="gap-1.5 h-8">
              {isImporting ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <Upload className="h-3.5 w-3.5" />}
              Import
            </Button>
          </div>
        </div>
      )}

      {/* Table */}
      <div className="flex-1 overflow-auto mt-3">
        {loading && !result ? (
          <div className="flex items-center justify-center py-12">
            <Loader2 className="h-6 w-6 animate-spin text-muted-foreground" />
          </div>
        ) : items.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-12 text-muted-foreground">
            <p className="text-sm">{search || activeFilterCount > 0 ? 'No records match your query' : 'No records yet'}</p>
            {!schema.read_only && !search && activeFilterCount === 0 && (
              <Button size="sm" variant="outline" onClick={handleCreate} className="mt-3 gap-1.5">
                <Plus className="h-3.5 w-3.5" />
                Create first {schema.name}
              </Button>
            )}
          </div>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                {!schema.read_only && (
                  <TableHead className="w-10">
                    <input
                      type="checkbox"
                      checked={items.length > 0 && selected.size === items.length}
                      onChange={toggleSelectAll}
                      className="h-4 w-4 rounded border-input"
                    />
                  </TableHead>
                )}
                {listFields.map((f) => (
                  <TableHead
                    key={f.column}
                    className={`cursor-pointer select-none hover:text-foreground ${cellAlignClass(f)}`}
                    onClick={() => toggleSort(f.column)}
                  >
                    <span className="flex items-center gap-1">
                      {fixLabel(f.label)}
                      {sortColumn === f.column ? (
                        sortDir === 'asc' ? <ArrowUp className="h-3 w-3" /> : <ArrowDown className="h-3 w-3" />
                      ) : (
                        <ArrowUpDown className="h-3 w-3 opacity-30" />
                      )}
                    </span>
                  </TableHead>
                ))}
                <TableHead className="w-20" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((row, idx) => {
                const id = readField(row, pkField!) as number
                return (
                  <TableRow key={id ?? idx} className={selected.has(id) ? 'bg-muted/50' : ''}>
                    {!schema.read_only && (
                      <TableCell>
                        <input
                          type="checkbox"
                          checked={selected.has(id)}
                          onChange={() => toggleSelect(id)}
                          className="h-4 w-4 rounded border-input"
                        />
                      </TableCell>
                    )}
                    {listFields.map((f) => {
                      const val = readField(row, f)
                      return (
                        <TableCell key={f.column} className={`text-sm ${cellAlignClass(f)}`}>
                          {f.html_type === 'checkbox' ? (
                            <Badge variant={val ? 'default' : 'outline'} className="text-xs">
                              {val ? 'Yes' : 'No'}
                            </Badge>
                          ) : (
                            formatCellValue(val, f)
                          )}
                        </TableCell>
                      )
                    })}
                    <TableCell>
                      <div className="flex items-center justify-end gap-1">
                        <button
                          onClick={() => handleEdit(row)}
                          className="p-1 rounded hover:bg-muted text-muted-foreground hover:text-foreground"
                          title={schema.read_only ? 'View' : 'Edit'}
                        >
                          <Pencil className="h-3.5 w-3.5" />
                        </button>
                        {!schema.read_only && (
                          <button
                            onClick={() => setDeleteId(id)}
                            className="p-1 rounded hover:bg-destructive/10 text-muted-foreground hover:text-destructive"
                            title="Delete"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </button>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                )
              })}
            </TableBody>
          </Table>
        )}
      </div>

      {/* Pagination */}
      {result && totalPages > 0 && (
        <div className="flex items-center justify-between pt-3 border-t text-sm">
          <div className="flex items-center gap-2 text-muted-foreground">
            <span>{total.toLocaleString()} record{total !== 1 ? 's' : ''}</span>
            <span className="text-border">|</span>
            <select
              value={pageSize}
              onChange={(e) => { setPageSize(Number(e.target.value)); setPage(1) }}
              className="h-7 rounded border border-input bg-background px-1.5 text-xs"
            >
              {PAGE_SIZES.map((s) => (
                <option key={s} value={s}>{s} / page</option>
              ))}
            </select>
          </div>
          <div className="flex items-center gap-1">
            <Button variant="outline" size="icon" className="h-7 w-7" disabled={page <= 1} onClick={() => setPage(1)}>
              <ChevronsLeft className="h-3.5 w-3.5" />
            </Button>
            <Button variant="outline" size="icon" className="h-7 w-7" disabled={page <= 1} onClick={() => setPage(page - 1)}>
              <ChevronLeft className="h-3.5 w-3.5" />
            </Button>
            <span className="px-2 text-xs tabular-nums">
              {page} / {totalPages}
            </span>
            <Button variant="outline" size="icon" className="h-7 w-7" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
              <ChevronRight className="h-3.5 w-3.5" />
            </Button>
            <Button variant="outline" size="icon" className="h-7 w-7" disabled={page >= totalPages} onClick={() => setPage(totalPages)}>
              <ChevronsRight className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>
      )}

      {/* Create/Edit Dialog */}
      <RecordForm
        open={formOpen}
        onClose={() => setFormOpen(false)}
        schema={schema}
        record={editingRecord}
        onSave={handleSave}
      />

      {/* Delete Confirmation Dialog */}
      <Dialog open={deleteId !== null} onOpenChange={(val) => !val && setDeleteId(null)}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Delete {schema.name}</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete record #{deleteId}? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteId(null)} disabled={deleting}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={confirmDelete} disabled={deleting}>
              {deleting ? <Loader2 className="mr-2 h-4 w-4 animate-spin" /> : null}
              Delete
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
