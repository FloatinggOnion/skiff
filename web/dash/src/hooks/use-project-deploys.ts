import { useInfiniteQuery, useQuery } from '@tanstack/react-query'
import { deploysService, type DeployCursor, type DeployFilters } from '@/services/api.service'
import { queryKeys } from '@/constants/query-keys'

const PAGE = 30

// A project's deployments and its previews', keyset-paginated for the Deployments tab.
export function useProjectDeploys(app: string, filters: DeployFilters) {
  return useInfiniteQuery({
    queryKey: queryKeys.deploysProject(app, filters),
    queryFn: ({ pageParam }) => deploysService.listProject(app, filters, pageParam, PAGE),
    initialPageParam: undefined as DeployCursor | undefined,
    getNextPageParam: (last) =>
      last.length === PAGE
        ? { before: last[last.length - 1].started, beforeId: last[last.length - 1].id }
        : undefined,
    enabled: !!app,
    refetchInterval: (query) =>
      query.state.data?.pages.some((p) => p.some((d) => d.status === 'building')) ? 5000 : 20000,
  })
}

export function useDeployBranches(app: string) {
  return useQuery({
    queryKey: queryKeys.deployBranches(app),
    queryFn: () => deploysService.branches(app),
    enabled: !!app,
  })
}

export function useDeployment(app: string, id: string) {
  return useQuery({
    queryKey: queryKeys.deployOne(app, id),
    queryFn: () => deploysService.one(app, id),
    enabled: !!app && !!id,
    retry: false,
    refetchInterval: (query) => (query.state.data?.status === 'building' ? 3000 : 20000),
  })
}
