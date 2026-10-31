import { useQuery } from '@tanstack/react-query'
import { logsService, type LogHistory } from '@/services/api.service'
import { queryKeys } from '@/constants/query-keys'

// Saved runtime logs for one app, searched server-side. until=0 means "up to now" and keeps refreshing.
export function useLogHistory(
  app: string | null,
  q: string,
  errors: boolean,
  since: number,
  until: number,
) {
  return useQuery<LogHistory>({
    queryKey: queryKeys.logHistory(app ?? '', q, errors, since, until),
    queryFn: () => logsService.history(app!, { q, errors, since, until: until || undefined }),
    enabled: !!app,
    refetchInterval: until ? false : 10000,
  })
}
