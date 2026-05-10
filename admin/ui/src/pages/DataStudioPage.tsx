import { useMemo, useState } from 'react'
import { PageBody, PageHeader } from '@/components/Layout'
import {
  useDeleteRecord,
  useModels,
  useRecords,
  useSaveRecord,
  useSchema,
} from '@/hooks/useDataStudio'
import type { ModelField, ModelInfo, Record as PBRecord } from '@/gen/nucleus/admin/v1/admin_pb'

export function DataStudioPage() {
  const [selectedModel, setSelectedModel] = useState<string | null>(null)
  const [page, setPage] = useState(1)
  const [editing, setEditing] = useState<{ id?: string; values: Record<string, string> } | null>(null)
  const pageSize = 20

  const models = useModels(false)
  const schema = useSchema(selectedModel)
  const records = useRecords(
    selectedModel ? { modelName: selectedModel, page, pageSize } : null,
  )

  const deleteMut = useDeleteRecord(selectedModel ?? '')
  const saveMut = useSaveRecord(selectedModel ?? '')

  const listFields = useMemo<ModelField[]>(() => {
    if (!schema.data) return []
    const inList = schema.data.fields.filter((f) => f.isInList && !f.isExcluded)
    return inList.length > 0 ? inList : schema.data.fields.filter((f) => !f.isExcluded).slice(0, 6)
  }, [schema.data])

  return (
    <>
      <PageHeader
        title="Data Studio"
        subtitle="Browse and edit registered models. Operations execute on a connected agent — signals, validation, and tenant filters apply."
      />
      <PageBody>
        <div className="grid grid-cols-12 gap-4">
          <aside className="col-span-3 rounded-lg border border-zinc-800 bg-zinc-900/40 p-3">
            <div className="mb-2 text-xs uppercase tracking-wider text-zinc-500">
              Registered models
            </div>
            {models.isLoading && <div className="text-sm text-zinc-500">Loading…</div>}
            {models.isError && (
              <div className="text-sm text-rose-400">{models.error.message}</div>
            )}
            <ul className="space-y-1">
              {(models.data ?? []).map((m: ModelInfo) => (
                <li key={m.name}>
                  <button
                    type="button"
                    onClick={() => {
                      setSelectedModel(m.name)
                      setPage(1)
                      setEditing(null)
                    }}
                    className={[
                      'w-full rounded px-2 py-1.5 text-left text-sm transition-colors',
                      selectedModel === m.name
                        ? 'bg-zinc-800 text-zinc-100'
                        : 'text-zinc-400 hover:bg-zinc-800/60 hover:text-zinc-100',
                    ].join(' ')}
                  >
                    {m.name}
                  </button>
                </li>
              ))}
              {!models.isLoading && (models.data ?? []).length === 0 && (
                <li className="text-sm text-zinc-500">
                  No agents are reporting models. Connect an agent with{' '}
                  <code className="text-zinc-300">Registry</code> wired in its config.
                </li>
              )}
            </ul>
          </aside>

          <section className="col-span-9 space-y-3">
            {!selectedModel && (
              <div className="rounded-lg border border-zinc-800 bg-zinc-900/40 p-6 text-sm text-zinc-400">
                Select a model on the left to browse its records.
              </div>
            )}

            {selectedModel && (
              <>
                <div className="flex items-center justify-between">
                  <h2 className="text-lg font-semibold">{selectedModel}</h2>
                  <button
                    type="button"
                    onClick={() => setEditing({ values: {} })}
                    className="rounded-md border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-sm text-zinc-200 hover:bg-zinc-800"
                  >
                    + New record
                  </button>
                </div>

                {records.isLoading && (
                  <div className="rounded-lg border border-zinc-800 bg-zinc-900/40 p-6 text-sm text-zinc-500">
                    Loading…
                  </div>
                )}
                {records.isError && (
                  <div className="rounded-lg border border-rose-800 bg-rose-950/50 p-4 text-sm text-rose-300">
                    {records.error.message}
                  </div>
                )}

                {records.data && (
                  <RecordTable
                    records={records.data.items}
                    fields={listFields}
                    onEdit={(rec) => {
                      const values: Record<string, string> = {}
                      for (const [k, v] of Object.entries(rec.valuesJson)) {
                        values[k] = v
                      }
                      const id = unquoteJSON(values['ID'] ?? values['Id'] ?? values['id'] ?? '')
                      setEditing({ id, values })
                    }}
                    onDelete={(rec) => {
                      const id = unquoteJSON(
                        rec.valuesJson['ID'] ?? rec.valuesJson['Id'] ?? rec.valuesJson['id'] ?? '',
                      )
                      if (!id) return
                      if (confirm(`Delete record ${id}?`)) {
                        deleteMut.mutate(id)
                      }
                    }}
                  />
                )}

                {records.data && (
                  <Pager
                    page={records.data.page || 1}
                    hasMore={records.data.hasMore}
                    onPrev={() => setPage((p) => Math.max(1, p - 1))}
                    onNext={() => setPage((p) => p + 1)}
                  />
                )}
              </>
            )}
          </section>
        </div>

        {editing !== null && schema.data && selectedModel && (
          <RecordEditor
            schema={schema.data.fields}
            initial={editing.values}
            onCancel={() => setEditing(null)}
            onSave={(values) => {
              saveMut.mutate(
                editing.id !== undefined && editing.id !== ''
                  ? { id: editing.id, values }
                  : { values },
                {
                  onSuccess: () => setEditing(null),
                },
              )
            }}
            saving={saveMut.isPending}
            error={saveMut.error?.message}
          />
        )}
      </PageBody>
    </>
  )
}

function RecordTable(props: {
  records: PBRecord[]
  fields: ModelField[]
  onEdit: (rec: PBRecord) => void
  onDelete: (rec: PBRecord) => void
}) {
  return (
    <div className="overflow-x-auto rounded-lg border border-zinc-800">
      <table className="w-full text-sm">
        <thead className="bg-zinc-900/60 text-left text-xs uppercase tracking-wider text-zinc-500">
          <tr>
            {props.fields.map((f) => (
              <th key={f.name} className="px-3 py-2 font-medium">
                {f.label || f.name}
              </th>
            ))}
            <th className="px-3 py-2 text-right font-medium">Actions</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-zinc-800">
          {props.records.length === 0 && (
            <tr>
              <td
                colSpan={props.fields.length + 1}
                className="px-3 py-6 text-center text-zinc-500"
              >
                No records.
              </td>
            </tr>
          )}
          {props.records.map((rec, idx) => (
            <tr key={idx} className="hover:bg-zinc-900/40">
              {props.fields.map((f) => (
                <td key={f.name} className="px-3 py-1.5 font-mono text-xs text-zinc-300">
                  {formatCell(rec.valuesJson[f.name])}
                </td>
              ))}
              <td className="px-3 py-1.5 text-right">
                <button
                  type="button"
                  onClick={() => props.onEdit(rec)}
                  className="rounded border border-zinc-700 bg-zinc-900 px-2 py-0.5 text-xs text-zinc-200 hover:bg-zinc-800"
                >
                  Edit
                </button>{' '}
                <button
                  type="button"
                  onClick={() => props.onDelete(rec)}
                  className="rounded border border-rose-700 bg-rose-950/40 px-2 py-0.5 text-xs text-rose-300 hover:bg-rose-900/40"
                >
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

function Pager(props: { page: number; hasMore: boolean; onPrev: () => void; onNext: () => void }) {
  return (
    <div className="flex items-center justify-between text-sm text-zinc-400">
      <button
        type="button"
        disabled={props.page <= 1}
        onClick={props.onPrev}
        className="rounded border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-xs text-zinc-200 disabled:opacity-40"
      >
        ← Prev
      </button>
      <span>Page {props.page}</span>
      <button
        type="button"
        disabled={!props.hasMore}
        onClick={props.onNext}
        className="rounded border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-xs text-zinc-200 disabled:opacity-40"
      >
        Next →
      </button>
    </div>
  )
}

function RecordEditor(props: {
  schema: ModelField[]
  initial: Record<string, string>
  onCancel: () => void
  onSave: (values: Record<string, string>) => void
  saving: boolean
  error: string | undefined
}) {
  const [values, setValues] = useState<Record<string, string>>(() => ({ ...props.initial }))

  const editable = props.schema.filter((f) => !f.isExcluded && !f.isReadonly)

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 p-4">
      <div className="w-full max-w-2xl rounded-lg border border-zinc-800 bg-zinc-950 shadow-xl">
        <header className="flex items-center justify-between border-b border-zinc-800 px-5 py-3">
          <h3 className="text-sm font-semibold">
            {props.initial['ID'] ?? props.initial['Id'] ?? props.initial['id'] ? 'Edit' : 'New'}{' '}
            record
          </h3>
          <button
            type="button"
            onClick={props.onCancel}
            className="text-zinc-500 hover:text-zinc-200"
          >
            ✕
          </button>
        </header>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            props.onSave(values)
          }}
          className="space-y-3 p-5"
        >
          {editable.map((f) => (
            <FieldEditor
              key={f.name}
              field={f}
              value={values[f.name] ?? ''}
              onChange={(v) => setValues((prev) => ({ ...prev, [f.name]: v }))}
            />
          ))}
          {props.error !== undefined && (
            <div className="rounded border border-rose-800 bg-rose-950/40 px-3 py-2 text-xs text-rose-300">
              {props.error}
            </div>
          )}
          <footer className="flex items-center justify-end gap-2 border-t border-zinc-800 pt-3">
            <button
              type="button"
              onClick={props.onCancel}
              className="rounded border border-zinc-700 bg-zinc-900 px-3 py-1.5 text-sm text-zinc-200 hover:bg-zinc-800"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={props.saving}
              className="rounded border border-emerald-700 bg-emerald-900/40 px-3 py-1.5 text-sm text-emerald-200 hover:bg-emerald-900/60 disabled:opacity-50"
            >
              {props.saving ? 'Saving…' : 'Save'}
            </button>
          </footer>
        </form>
      </div>
    </div>
  )
}

function FieldEditor(props: { field: ModelField; value: string; onChange: (v: string) => void }) {
  const isText = props.field.htmlType === 'textarea'
  return (
    <label className="block text-sm">
      <span className="mb-1 block text-xs uppercase tracking-wider text-zinc-500">
        {props.field.label || props.field.name}
        {props.field.isRequired && <span className="text-rose-400"> *</span>}
      </span>
      {isText ? (
        <textarea
          value={parseDisplay(props.value)}
          onChange={(e) => props.onChange(JSON.stringify(e.target.value))}
          className="w-full rounded border border-zinc-700 bg-zinc-900 px-2 py-1 font-mono text-xs text-zinc-100"
          rows={4}
        />
      ) : (
        <input
          type="text"
          value={parseDisplay(props.value)}
          onChange={(e) => {
            const raw = e.target.value
            // Encode as JSON: numeric strings become numbers, "true"/"false"
            // become booleans, everything else becomes a JSON string.
            props.onChange(encodeUserValue(raw, props.field.goType))
          }}
          className="w-full rounded border border-zinc-700 bg-zinc-900 px-2 py-1 font-mono text-xs text-zinc-100"
        />
      )}
    </label>
  )
}

function parseDisplay(raw: string): string {
  if (!raw) return ''
  try {
    const parsed: unknown = JSON.parse(raw)
    if (parsed === null || parsed === undefined) return ''
    if (typeof parsed === 'string') return parsed
    if (typeof parsed === 'number' || typeof parsed === 'boolean') return String(parsed)
    return JSON.stringify(parsed)
  } catch {
    return raw
  }
}

function encodeUserValue(raw: string, goType: string): string {
  if (raw === '') return ''
  const lower = goType.toLowerCase()
  if (lower.startsWith('int') || lower.startsWith('uint')) {
    const n = Number(raw)
    if (Number.isFinite(n)) return JSON.stringify(n)
  }
  if (lower.startsWith('float')) {
    const n = Number(raw)
    if (Number.isFinite(n)) return JSON.stringify(n)
  }
  if (lower === 'bool') {
    if (raw.toLowerCase() === 'true') return 'true'
    if (raw.toLowerCase() === 'false') return 'false'
  }
  return JSON.stringify(raw)
}

function formatCell(raw: string | undefined): string {
  if (!raw) return ''
  try {
    const v: unknown = JSON.parse(raw)
    if (v === null) return '∅'
    if (typeof v === 'string') {
      return v.length > 80 ? v.slice(0, 77) + '…' : v
    }
    if (typeof v === 'number' || typeof v === 'boolean') return String(v)
    return JSON.stringify(v)
  } catch {
    return raw
  }
}

function unquoteJSON(raw: string): string {
  try {
    const v: unknown = JSON.parse(raw)
    if (typeof v === 'string') return v
    if (typeof v === 'number') return String(v)
    return ''
  } catch {
    return raw
  }
}
