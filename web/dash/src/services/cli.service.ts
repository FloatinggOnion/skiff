import { BaseService } from '@/services/base.service'

export type CliLoginRequest = { code: string; host: string; ip: string; requested: number }

class CliService extends BaseService {
  pending(code: string) {
    return this.get<CliLoginRequest>('/cli/approve', { params: { code } })
  }

  approve(code: string) {
    return this.post('/cli/approve', undefined, { params: { code } })
  }

  deny(code: string) {
    return this.post('/cli/approve', undefined, { params: { code, deny: 1 } })
  }
}

export const cliService = new CliService()
