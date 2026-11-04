import { BaseService } from '@/services/base.service'

export type DbEngine = 'postgres' | 'mysql' | 'mongodb' | 'redis'

export type Backup = {
  id: string
  size: number
  trigger: string
  created: number
  offsite?: 'pending' | 'ok' | 'failed'
  checkStatus?: 'running' | 'passed' | 'failed' | 'skipped'
  checkDetail?: string
  checkItems?: number
  checkAt?: number
}

export type OffsiteTarget = {
  endpoint: string
  region: string
  bucket: string
  prefix: string
  accessKey: string
  secretKey?: string
  hasSecret?: boolean
}

export type UnmanagedDatabase = {
  container: string
  engine: DbEngine
  image: string
  volume: string
}

export type Database = {
  id: string
  name: string
  engine: DbEngine
  host: string
  port: number
  created: number
  state: string
  url: string
  attached: string[]
  public: boolean
  publicUrl?: string
  verifiedAt?: number
  checkFailed?: boolean
}

export type PitrStatus = {
  supported: boolean
  enabled: boolean
  paused?: string
  error?: string
  windowStart?: number
  size?: number
}

class DatabasesService extends BaseService {
  list() {
    return this.get<Database[]>('/databases')
  }

  unmanaged() {
    return this.get<UnmanagedDatabase[]>('/databases/unmanaged')
  }

  adopt(container: string, name: string) {
    return this.post<Database>('/databases/unmanaged', undefined, { params: { container, name } })
  }

  create(engine: DbEngine, name: string) {
    return this.post<Database>('/databases', { engine, name })
  }

  remove(id: string) {
    return this.delete('/databases', { params: { id } })
  }

  attach(id: string, app: string) {
    return this.post<Database>('/databases/attach', undefined, { params: { id, app } })
  }

  setPublic(id: string, on: boolean) {
    return this.post<Database>('/databases/public', undefined, {
      params: { id, on: on ? '1' : '0' },
    })
  }

  detach(id: string, app: string) {
    return this.delete<Database>('/databases/attach', { params: { id, app } })
  }

  offsite() {
    return this.get<OffsiteTarget>('/backups/offsite')
  }

  saveOffsite(t: OffsiteTarget) {
    return this.put('/backups/offsite', t)
  }

  testOffsite(t: OffsiteTarget) {
    return this.post('/backups/offsite', t)
  }

  disableOffsite() {
    return this.delete('/backups/offsite')
  }

  listBackups(dbId: string) {
    return this.get<Backup[]>('/backups', { params: { db: dbId } })
  }

  createBackup(dbId: string) {
    return this.post<Backup>('/backups', undefined, { params: { db: dbId } })
  }

  restoreBackup(id: string) {
    return this.post('/backups/restore', undefined, { params: { id } })
  }

  pitrStatus(dbId: string) {
    return this.get<PitrStatus>('/databases/pitr', { params: { db: dbId } })
  }

  setPitr(dbId: string, enabled: boolean) {
    return this.post<PitrStatus>(
      '/databases/pitr',
      { action: enabled ? 'enable' : 'disable' },
      { params: { db: dbId } },
    )
  }

  recoverToTime(dbId: string, at: number) {
    return this.post<PitrStatus>(
      '/databases/pitr',
      { action: 'restore', at },
      { params: { db: dbId } },
    )
  }

  checkBackup(id: string) {
    return this.post('/backups/check', undefined, { params: { id } })
  }

  deleteBackup(id: string) {
    return this.delete('/backups', { params: { id } })
  }

  backupDownloadUrl(id: string) {
    return `/api/backups/download?id=${encodeURIComponent(id)}`
  }
}

export const databasesService = new DatabasesService()
