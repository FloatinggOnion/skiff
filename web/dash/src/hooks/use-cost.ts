import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { costService, type CostReport } from '@/services/api.service'
import { queryKeys } from '@/constants/query-keys'

export function useCost() {
  return useQuery<CostReport>({
    queryKey: queryKeys.cost,
    queryFn: () => costService.report(),
    refetchInterval: 60000,
  })
}

export function useSetServerPrice() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ price, currency }: { price: number; currency: string }) =>
      costService.setPrice(price, currency),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.cost }),
  })
}
