import { BaseService } from '@/services/base.service'

export type InsightRange = '1h' | '24h' | '7d'

export type InsightPoint = {
  t: number
  req: number
  e4: number
  e5: number
  lat: number
  visitors: number
}

export type InsightRow = { key: string; count: number; extra?: number }

export type InsightMarker = {
  id: string
  t: number
  commit: string
  message: string
  trigger: string
  status: string
}

export type Insights = {
  range: InsightRange
  bucketSecs: number
  from: number
  requests: number
  errors4: number
  errors5: number
  p50: number
  p95: number
  p99: number
  visitors: number
  pageviews: number
  bots: number
  bytes: number
  series: InsightPoint[]
  paths: InsightRow[]
  pages: InsightRow[]
  errorPaths: InsightRow[]
  referrers: InsightRow[]
  deploys: InsightMarker[]
}

class InsightsService extends BaseService {
  project(app: string, range: InsightRange) {
    return this.get<Insights>('/insights', { params: { app, range } })
  }
}

export const insightsService = new InsightsService()

export type UptimeDay = { day: number; pct: number; checks: number }

export type Incident = { id: number; started: number; ended: number; cause: string }

export type Uptime = {
  up: boolean
  checked: number
  code: number
  ms: number
  pct24h: number
  pct7d: number
  pct30d: number
  days: UptimeDay[]
  incidents: Incident[]
}

class UptimeService extends BaseService {
  project(app: string) {
    return this.get<Uptime>('/uptime', { params: { app } })
  }
}

export const uptimeService = new UptimeService()
