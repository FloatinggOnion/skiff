import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '@/constants/query-keys'
import { serverService, type Overview } from '@/services/api.service'

export function useOverview() {
  return useQuery<Overview>({
    queryKey: queryKeys.overview,
    queryFn: () => serverService.overview(),
    refetchInterval: 8000,
  })
}
