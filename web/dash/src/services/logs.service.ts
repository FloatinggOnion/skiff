import { BaseService } from '@/services/base.service'

export type LogLine = { t: string; container: string; stream: 'out' | 'err' | string; text: string }

export type LogHistory = { lines: LogLine[]; truncated: boolean }

export type LogQuery = { q?: string; errors?: boolean; since: number; until?: number }

export type LogDrain = {
  configured: boolean
  url: string
  headerName: string
  headerSet: boolean
  lastOk: number
  lastError: string
}

class LogsService extends BaseService {
  history(app: string, query: LogQuery) {
    return this.get<LogHistory>('/logs/history', {
      params: {
        app,
        q: query.q || undefined,
        errors: query.errors ? 1 : undefined,
        since: query.since,
        until: query.until,
      },
    })
  }

  drain() {
    return this.get<LogDrain>('/log-drain')
  }

  saveDrain(drain: { url: string; headerName: string; headerValue: string }) {
    return this.put('/log-drain', drain)
  }

  removeDrain() {
    return this.delete('/log-drain')
  }

  testDrain() {
    return this.post('/log-drain/test', null)
  }
}

export const logsService = new LogsService()
