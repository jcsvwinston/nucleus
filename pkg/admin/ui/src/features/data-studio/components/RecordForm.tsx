import { useState, useEffect } from 'react'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter, DialogDescription } from '@/components/ui/dialog'
import type { SchemaField, ModelSchema, Record as AppRecord } from '@/types'
import { Loader2 } from 'lucide-react'

interface Props {
  open: boolean
  onClose: () => void
  schema: ModelSchema
  record: AppRecord | null
  onSave: (data: AppRecord) => Promise<void>
}

function editableFields(schema: ModelSchema, isEdit: boolean): SchemaField[] {
  return schema.fields.filter((f) => {
    if (f.is_excluded) return false
    if (f.is_pk) return false
    if (f.is_readonly) return false
    if (f.is_tenant_field) return false
    if (isEdit && f.name === 'CreatedAt') return false
    return true
  })
}

function displayFields(schema: ModelSchema): SchemaField[] {
  return schema.fields.filter((f) => {
    if (f.is_excluded) return false
    if (f.is_pk && f.is_readonly) return true
    if (f.is_readonly) return true
    return false
  })
}

function readField(record: AppRecord, field: SchemaField): any {
  if (field.column in record) return record[field.column]
  if (field.name in record) return record[field.name]
  const lc = field.column.toLowerCase()
  for (const key of Object.keys(record)) {
    if (key.toLowerCase() === lc) return record[key]
  }
  return undefined
}

function getFieldValue(record: AppRecord | null, field: SchemaField): string {
  if (!record) return ''
  const val = readField(record, field) ?? ''
  if (val === null || val === undefined) return ''
  if (field.html_type === 'checkbox') return val ? 'true' : 'false'
  if (field.html_type === 'datetime-local' && typeof val === 'string' && val.length > 16) {
    return val.slice(0, 16)
  }
  return String(val)
}

function FieldInput({
  field,
  value,
  onChange,
}: {
  field: SchemaField
  value: string
  onChange: (val: string) => void
}) {
  const htmlType = field.html_type || 'text'

  if (field.choices && field.choices.length > 0) {
    return (
      <select
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className="flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
      >
        <option value="">— Select —</option>
        {field.choices.map((c) => (
          <option key={c.value} value={c.value}>
            {c.label || c.value}
          </option>
        ))}
      </select>
    )
  }

  if (htmlType === 'textarea') {
    return (
      <textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        rows={4}
        className="flex w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring resize-y min-h-[80px]"
      />
    )
  }

  if (htmlType === 'checkbox') {
    const checked = value === 'true' || value === '1'
    return (
      <div className="flex items-center gap-2 h-10">
        <input
          type="checkbox"
          checked={checked}
          onChange={(e) => onChange(e.target.checked ? 'true' : 'false')}
          className="h-4 w-4 rounded border-input"
        />
        <span className="text-sm text-muted-foreground">{checked ? 'Yes' : 'No'}</span>
      </div>
    )
  }

  return (
    <Input
      type={htmlType === 'number' ? 'number' : htmlType === 'email' ? 'email' : htmlType === 'password' ? 'password' : htmlType === 'datetime-local' ? 'datetime-local' : 'text'}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      required={field.is_required}
      placeholder={field.label}
    />
  )
}

export default function RecordForm({ open, onClose, schema, record, onSave }: Props) {
  const isEdit = record !== null
  const fields = editableFields(schema, isEdit)
  const readonlyFields = displayFields(schema)
  const [formData, setFormData] = useState<Record<string, string>>({})
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!open) return
    const data: Record<string, string> = {}
    for (const f of fields) {
      data[f.column] = getFieldValue(record, f)
    }
    setFormData(data)
    setError(null)
  }, [open, record, schema])

  const updateField = (column: string, value: string) => {
    setFormData((prev) => ({ ...prev, [column]: value }))
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setSaving(true)
    setError(null)

    try {
      const payload: AppRecord = {}
      for (const f of fields) {
        const raw = formData[f.column]
        if (raw === undefined || raw === '') {
          if (f.html_type === 'checkbox') payload[f.column] = false
          continue
        }
        if (f.html_type === 'number') {
          payload[f.column] = f.type.includes('int') ? parseInt(raw, 10) : parseFloat(raw)
        } else if (f.html_type === 'checkbox') {
          payload[f.column] = raw === 'true' || raw === '1'
        } else {
          payload[f.column] = raw
        }
      }
      await onSave(payload)
      onClose()
    } catch (err: any) {
      setError(err.message || 'Failed to save record')
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={(val) => !val && onClose()}>
      <DialogContent className="max-w-lg max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>
            {isEdit ? 'Edit' : 'Create'} {schema.name}
          </DialogTitle>
          <DialogDescription>
            {isEdit
              ? `Update the fields below for this ${schema.name} record.`
              : `Fill in the fields below to create a new ${schema.name} record.`}
          </DialogDescription>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4">
          {isEdit && readonlyFields.length > 0 && (
            <div className="space-y-2 pb-3 border-b">
              {readonlyFields.map((f) => (
                <div key={f.name} className="flex items-center gap-2 text-sm">
                  <span className="text-muted-foreground w-24 flex-shrink-0">{f.label}:</span>
                  <span className="font-mono text-xs">{getFieldValue(record, f) || '—'}</span>
                </div>
              ))}
            </div>
          )}

          {fields.map((f) => (
            <div key={f.column} className="space-y-1.5">
              <Label htmlFor={`field-${f.column}`} className="flex items-center gap-1.5">
                {f.label}
                {f.is_required && <span className="text-destructive text-xs">*</span>}
                {f.is_fk && f.fk_model && (
                  <span className="text-[10px] text-muted-foreground">FK → {f.fk_model}</span>
                )}
              </Label>
              <FieldInput
                field={f}
                value={formData[f.column] ?? ''}
                onChange={(val) => updateField(f.column, val)}
              />
            </div>
          ))}

          {error && (
            <div className="rounded-md bg-destructive/10 border border-destructive/20 px-3 py-2 text-sm text-destructive">
              {error}
            </div>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose} disabled={saving}>
              Cancel
            </Button>
            <Button type="submit" disabled={saving || schema.read_only}>
              {saving ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  Saving...
                </>
              ) : isEdit ? (
                'Update'
              ) : (
                'Create'
              )}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
