import { useCallback } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { mailService, type MailResponse } from '@/services/api.service'
import { queryKeys } from '@/constants/query-keys'

export function useMail() {
  const qc = useQueryClient()
  const { data, isPending, isError } = useQuery<MailResponse>({
    queryKey: queryKeys.mail,
    queryFn: () => mailService.list(),
    refetchInterval: 15000, // pick up MX/SPF propagation
  })

  const reload = useCallback(() => qc.invalidateQueries({ queryKey: queryKeys.mail }), [qc])

  const add = useCallback(
    async (domain: string, alias: string, destinations: string[]) => {
      await mailService.add(domain, alias, destinations)
      reload()
    },
    [reload],
  )

  const remove = useCallback(
    async (id: string) => {
      await mailService.remove(id)
      reload()
    },
    [reload],
  )

  return {
    forwards: data?.forwards ?? [],
    domains: data?.domains ?? [],
    mxHost: data?.mxHost ?? '',
    serverIp: data?.serverIp ?? '',
    relay: data?.relay ?? false,
    isPending,
    isError,
    add,
    remove,
  }
}
