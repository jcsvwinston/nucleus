// useNodes is the unary ListNodes wrapper. TanStack Query handles the
// short-poll cadence (3s) and pause-on-hidden-tab automatically.

import { useQuery } from '@tanstack/react-query'
import { controlClient } from '@/lib/transport'
import type { NodeInfo } from '@/gen/nucleus/admin/v1/admin_pb'

export interface UseNodesResult {
  nodes: NodeInfo[]
  isLoading: boolean
  isError: boolean
  error: Error | null
  refetch: () => void
}

export function useNodes(): UseNodesResult {
  const query = useQuery({
    queryKey: ['admin', 'nodes'],
    queryFn: async () => {
      const resp = await controlClient.listNodes({})
      return resp.nodes
    },
    refetchInterval: 3000,
    refetchIntervalInBackground: false,
    staleTime: 1000,
  })

  return {
    nodes: query.data ?? [],
    isLoading: query.isLoading,
    isError: query.isError,
    error: query.error,
    refetch: () => {
      void query.refetch()
    },
  }
}
