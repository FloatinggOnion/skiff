import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  serverService,
  type ControlPlane,
  type GCReport,
  type ServerInfo,
} from '@/services/api.service'
import { queryKeys } from '@/constants/query-keys'

// Polls the box's live metrics for the Server page.
export function useServer() {
  return useQuery<ServerInfo>({
    queryKey: queryKeys.server,
    queryFn: () => serverService.info(),
    refetchInterval: 4000,
  })
}

export function useReclaimSpace() {
  const qc = useQueryClient()
  return useMutation<GCReport>({
    mutationFn: () => serverService.reclaim(),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.server }),
  })
}

export function useControlPlane() {
  const qc = useQueryClient()
  const query = useQuery<ControlPlane>({
    queryKey: queryKeys.controlPlane,
    queryFn: () => serverService.controlPlane(),
    retry: false,
  })
  const backup = useMutation({
    mutationFn: () => serverService.backupControlPlane(),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.controlPlane }),
  })
  const reveal = useMutation({ mutationFn: () => serverService.revealRecoveryKey() })
  return { query, backup, reveal, downloadUrl: serverService.controlPlaneDownloadUrl() }
}
