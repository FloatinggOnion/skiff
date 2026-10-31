import { useCallback } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  databasesService,
  type Database,
  type DbEngine,
  type UnmanagedDatabase,
} from '@/services/api.service'
import { queryKeys } from '@/constants/query-keys'

export function useDatabases() {
  const qc = useQueryClient()
  const {
    data: databases = [],
    isPending,
    isError,
  } = useQuery<Database[]>({
    queryKey: queryKeys.databases,
    queryFn: () => databasesService.list(),
    refetchInterval: 8000,
  })

  const reload = useCallback(() => qc.invalidateQueries({ queryKey: queryKeys.databases }), [qc])

  const create = useCallback(
    async (engine: DbEngine, name: string) => {
      await databasesService.create(engine, name)
      reload()
    },
    [reload],
  )

  const remove = useCallback(
    async (id: string) => {
      await databasesService.remove(id)
      reload()
    },
    [reload],
  )

  const attach = useCallback(
    async (id: string, app: string) => {
      await databasesService.attach(id, app)
      reload()
    },
    [reload],
  )

  const detach = useCallback(
    async (id: string, app: string) => {
      await databasesService.detach(id, app)
      reload()
    },
    [reload],
  )

  const setPublic = useCallback(
    async (id: string, on: boolean) => {
      await databasesService.setPublic(id, on)
      reload()
    },
    [reload],
  )

  return { databases, isPending, isError, create, remove, attach, detach, setPublic }
}

// Database containers running on the box that weren't created through Skiff (owner-only; empty for everyone else).
export function useUnmanagedDatabases() {
  const qc = useQueryClient()
  const { data: found = [] } = useQuery<UnmanagedDatabase[]>({
    queryKey: queryKeys.databasesUnmanaged,
    queryFn: () => databasesService.unmanaged(),
    refetchInterval: 30000,
  })
  const adopt = useMutation({
    mutationFn: ({ container, name }: { container: string; name: string }) =>
      databasesService.adopt(container, name),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: queryKeys.databases })
    },
  })
  return { found, adopt }
}
