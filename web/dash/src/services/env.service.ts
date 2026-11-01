import { BaseService } from '@/services/base.service'

export type EnvVar = { key: string; value: string; build: boolean }

class EnvService extends BaseService {
  // scope 'preview' is the project's overrides for its previews.
  list(app: string, scope?: 'preview') {
    return this.get<EnvVar[]>('/env', { params: { app, scope } })
  }

  save(app: string, vars: EnvVar[], scope?: 'preview') {
    return this.put('/env', { vars }, { params: { app, scope } })
  }

  // Team-wide shared variables, merged into every app on its next deploy.
  sharedList() {
    return this.get<EnvVar[]>('/shared-env')
  }

  sharedSave(vars: EnvVar[]) {
    return this.put('/shared-env', { vars })
  }
}

export const envService = new EnvService()
