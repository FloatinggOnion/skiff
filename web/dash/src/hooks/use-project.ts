import { useQuery, useQueryClient } from '@tanstack/react-query'
import { projectsService, type ProjectDetail } from '@/services/api.service'
import { queryKeys } from '@/constants/query-keys'
import { useCallback } from 'react'

export function useProject(name: string) {
  const qc = useQueryClient()
  const { data: project = null, isPending } = useQuery<ProjectDetail>({
    queryKey: queryKeys.project(name),
    queryFn: () => projectsService.detail(name),
    enabled: !!name,
    // A preview can start from a push at any time, so keep watching while one is building.
    refetchInterval: (query) =>
      query.state.data?.previews?.some((pv) => pv.status === 'building') ? 5000 : 30000,
  })

  const reload = useCallback(
    () => qc.invalidateQueries({ queryKey: queryKeys.project(name) }),
    [qc, name],
  )

  return { project, isPending, reload }
}
