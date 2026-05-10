// TanStack Query wrappers around DataStudioService. Mutations
// invalidate the related query keys so the UI auto-refreshes after a
// CRUD operation.

import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { dataStudioClient } from '@/lib/transport'
import type { ModelInfo, ModelSchema, PaginatedRecords, Record as PBRecord } from '@/gen/nucleus/admin/v1/admin_pb'

export function useModels(includeCounts = false) {
  return useQuery({
    queryKey: ['data-studio', 'models', includeCounts],
    queryFn: async (): Promise<ModelInfo[]> => {
      const resp = await dataStudioClient.listModels({ includeCounts })
      return resp.models
    },
    staleTime: 10_000,
  })
}

export function useSchema(modelName: string | null) {
  return useQuery({
    queryKey: ['data-studio', 'schema', modelName],
    enabled: !!modelName,
    queryFn: async (): Promise<ModelSchema> => {
      return dataStudioClient.getSchema({ modelName: modelName ?? '' })
    },
    staleTime: 60_000,
  })
}

export interface ListRecordsParams {
  modelName: string
  page: number
  pageSize: number
  search?: string
  orderBy?: string
}

export function useRecords(params: ListRecordsParams | null) {
  return useQuery({
    queryKey: [
      'data-studio',
      'records',
      params?.modelName,
      params?.page,
      params?.pageSize,
      params?.search,
      params?.orderBy,
    ],
    enabled: !!params,
    queryFn: async (): Promise<PaginatedRecords> => {
      if (!params) throw new Error('params required')
      return dataStudioClient.listRecords({
        modelName: params.modelName,
        page: params.page,
        pageSize: params.pageSize,
        search: params.search ?? '',
        orderBy: params.orderBy ?? '',
      })
    },
    staleTime: 0,
  })
}

export function useDeleteRecord(modelName: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (id: string): Promise<boolean> => {
      const resp = await dataStudioClient.deleteRecord({ modelName, id })
      return resp.deleted
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['data-studio', 'records', modelName] })
    },
  })
}

export interface SaveRecordVars {
  id?: string
  values: Record<string, string>
}

export function useSaveRecord(modelName: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (vars: SaveRecordVars): Promise<PBRecord> => {
      if (vars.id !== undefined && vars.id !== '') {
        return dataStudioClient.updateRecord({
          modelName,
          id: vars.id,
          record: { valuesJson: vars.values },
        })
      }
      return dataStudioClient.createRecord({
        modelName,
        record: { valuesJson: vars.values },
      })
    },
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['data-studio', 'records', modelName] })
    },
  })
}
