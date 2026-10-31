import { BaseService } from '@/services/base.service'

export type CostItem = {
  key: string
  kind: 'app' | 'database' | 'storage'
  name: string
  team?: string
  cpu: number
  mem: number
  share: number
  monthly: number
  egressGb: number
}

export type CostReport = {
  price: number
  currency: string
  cores: number
  memTotal: number
  memUsed: number
  items: CostItem[]
  system: number
  idle: number
  headroom: number
  medianMem: number
  diskUsed: number
  diskTotal: number
  diskPerDay: number
  daysToFull: number
  samples: number
}

class CostService extends BaseService {
  report() {
    return this.get<CostReport>('/cost')
  }

  setPrice(price: number, currency: string) {
    return this.put('/cost', { price, currency })
  }
}

export const costService = new CostService()
