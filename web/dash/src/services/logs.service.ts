import { BaseService } from '@/services/base.service'

export type LogLine = { t: string; container: string; stream: 'out' | 'err' | string; text: string }

export type LogHistory = { lines: LogLine[]; truncated: boolean }

export type LogQuery = { q?: string; errors?: boolean; since: number; until?: number }

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
}

export const logsService = new LogsService()
