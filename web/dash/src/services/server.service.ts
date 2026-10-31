import { BaseService } from '@/services/base.service'

export type ResourceUsage = { total: number; used: number }

export type ContainerStat = {
  name: string
  image: string
  cpuPct: number
  memUsed: number
  memPct: number
}

export type ServerInfo = {
  hostname: string
  os: string
  uptime: number
  load: number[]
  cpuCount: number
  cpuPct: number
  mem: ResourceUsage
  disk: ResourceUsage
  docker: string
  containers: ContainerStat[]
}

export type GCReport = {
  removed: string[]
  freed: number
  diskUsed: number
  diskTotal: number
  ran: number
}

export type ControlPlaneBackup = { key: string; size: number; created: number }

export type ControlPlane = {
  bucket: string
  backups: ControlPlaneBackup[]
  recoveryKey?: string
}

class ServerService extends BaseService {
  info() {
    return this.get<ServerInfo>('/server')
  }

  reclaim() {
    return this.post<GCReport>('/server/gc')
  }

  controlPlane() {
    return this.get<ControlPlane>('/control-plane')
  }

  revealRecoveryKey() {
    return this.get<ControlPlane>('/control-plane', { params: { reveal: 1 } })
  }

  backupControlPlane() {
    return this.post<ControlPlaneBackup>('/control-plane')
  }

  controlPlaneDownloadUrl() {
    return '/api/control-plane?download=1'
  }
}

export const serverService = new ServerService()
