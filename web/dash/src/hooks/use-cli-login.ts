import { useMutation, useQuery } from '@tanstack/react-query'
import { cliService } from '@/services/api.service'
import { queryKeys } from '@/constants/query-keys'

export function useCliLogin(code: string) {
  const pending = useQuery({
    queryKey: queryKeys.cliLogin(code),
    queryFn: () => cliService.pending(code),
    enabled: !!code,
    retry: false,
  })
  const approve = useMutation({ mutationFn: () => cliService.approve(code) })
  const deny = useMutation({ mutationFn: () => cliService.deny(code) })
  return { pending, approve, deny }
}
