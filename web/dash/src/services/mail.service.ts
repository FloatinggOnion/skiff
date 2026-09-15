import { BaseService } from '@/services/base.service'

export type MailForward = {
  id: string
  domain: string
  alias: string
  destinations: string[]
  created: number
}

export type MailDomain = {
  domain: string
  mx: boolean
  spf: boolean
  mxHosts?: string[]
}

export type MailResponse = {
  mxHost: string
  serverIp: string
  relay: boolean
  domains: MailDomain[]
  forwards: MailForward[]
}

class MailService extends BaseService {
  list() {
    return this.get<MailResponse>('/mail')
  }

  add(domain: string, alias: string, destinations: string[]) {
    return this.post<MailForward>('/mail', { domain, alias, destinations })
  }

  remove(id: string) {
    return this.delete('/mail', { params: { id } })
  }
}

export const mailService = new MailService()
