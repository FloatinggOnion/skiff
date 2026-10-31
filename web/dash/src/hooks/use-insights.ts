import { useQuery } from '@tanstack/react-query'
import { insightsService, type InsightRange, type Insights } from '@/services/api.service'
import { queryKeys } from '@/constants/query-keys'

export function useInsights(app: string, range: InsightRange) {
  return useQuery<Insights>({
    queryKey: queryKeys.insights(app, range),
    queryFn: () => insightsService.project(app, range),
    refetchInterval: range === '1h' ? 15000 : 60000,
  })
}
