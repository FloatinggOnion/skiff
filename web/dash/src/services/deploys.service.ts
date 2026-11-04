import { BaseService } from '@/services/base.service'

export type Deploy = {
  id: string
  app: string
  commit: string
  message: string
  trigger: string
  status: string
  started: number
  rollbackable?: boolean
  finished?: number
  branch?: string
  actor?: string
  current?: boolean
  parent?: string
}

export type DeployDetail = Deploy & { url: string; origin?: 'cli' }

export type DeployFilters = {
  env?: 'production' | 'preview'
  status?: string
  branch?: string
  q?: string
}

// Keyset cursor into the deploy feed — the started + id of the last row seen.
export type DeployCursor = { before: number; beforeId: string }

class DeploysService extends BaseService {
  list(app: string, cursor?: DeployCursor, limit = 30) {
    return this.get<Deploy[]>('/deploys', {
      params: cursor
        ? { app, limit, before: cursor.before, beforeId: cursor.beforeId }
        : { app, limit },
    })
  }

  // A project's deployments and its previews', newest first.
  listProject(app: string, filters: DeployFilters, cursor?: DeployCursor, limit = 30) {
    return this.get<Deploy[]>('/deploys', {
      params: { app, previews: 1, limit, ...filters, ...cursor },
    })
  }

  one(app: string, id: string) {
    return this.get<DeployDetail>('/deploys/one', { params: { app, id } })
  }

  branches(app: string) {
    return this.get<string[]>('/deploys/branches', { params: { app } })
  }

  // Global build feed across every app, keyset-paginated newest-first. Omit the
  // cursor for the first page; pass the last row's { before, beforeId } to page
  // into older history.
  listAll(cursor?: DeployCursor, limit = 30) {
    return this.get<Deploy[]>('/deploys', {
      params: cursor ? { limit, before: cursor.before, beforeId: cursor.beforeId } : { limit },
    })
  }

  // Stop a build — cancels the live one, or force-clears a stuck one. Pass an
  // empty id to cancel whatever is currently building for the app.
  cancel(app: string, id: string) {
    return this.post('/cancel', null, { params: { app, id } })
  }
}

export const deploysService = new DeploysService()
