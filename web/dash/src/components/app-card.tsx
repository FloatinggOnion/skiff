import {
  GitBranch,
  ExternalLink,
  ScrollText,
  History,
  KeyRound,
  Square,
  MoreHorizontal,
  Terminal,
} from 'lucide-react'
import { Link } from 'react-router'
import type { App } from '@/services/api.service'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

type StatusStyle = { dot: string; ring: string; edge: string; pulse: boolean }

function statusStyle(state: string): StatusStyle {
  switch (state) {
    case 'running':
      return {
        dot: 'bg-success',
        ring: 'bg-success/15',
        edge: 'bg-success/50',
        pulse: false,
      }
    case 'exited':
    case 'missing':
      return { dot: 'bg-danger', ring: 'bg-danger/15', edge: 'bg-danger/50', pulse: false }
    case 'sleeping':
      return { dot: 'bg-info/70', ring: 'bg-info/10', edge: 'bg-info/30', pulse: false }
    case 'building':
    case 'starting':
    case 'created':
      return { dot: 'bg-warning', ring: 'bg-warning/15', edge: 'bg-warning/50', pulse: true }
    default:
      return { dot: 'bg-foreground/40', ring: 'bg-foreground/5', edge: 'bg-foreground/15', pulse: false }
  }
}

function fmtDate(unix?: number): string {
  if (!unix) return ''
  return new Date(unix * 1000).toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

export function AppCard({
  app,
  index = 0,
  onLogs,
  onHistory,
  onEnv,
  onStop,
}: {
  app: App
  index?: number
  onLogs: (name: string) => void
  onHistory: (name: string) => void
  onEnv: (name: string) => void
  onStop: (name: string) => void
}) {
  const s = statusStyle(app.state)
  const host = app.url.replace(/^https?:\/\//, '')
  const date = fmtDate(app.updated)

  return (
    <article
      style={{ animationDelay: `${Math.min(index, 10) * 45}ms` }}
      className="animate-rise group relative flex flex-col gap-3.5 overflow-hidden rounded-xl border border-foreground/8 bg-linear-to-b from-foreground/2.5 to-transparent p-4 transition-all duration-200 hover:-translate-y-0.5 hover:border-foreground/20 hover:shadow-[0_10px_40px_-15px_rgba(0,0,0,0.9)]"
    >
      <span className={'absolute top-0 left-0 h-full w-[2px] ' + s.edge} />

      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2.5">
          <span className="text-foreground grid h-8 w-8 shrink-0 place-items-center rounded-md bg-linear-to-br from-foreground/20 to-foreground/5 text-sm font-semibold">
            {app.name.charAt(0).toUpperCase()}
          </span>
          <div className="min-w-0">
            <Link
              to={'/projects/' + app.name}
              className="block truncate text-sm font-semibold tracking-tight hover:underline"
            >
              {app.name}
            </Link>
            <a
              href={app.url}
              target="_blank"
              rel="noreferrer"
              className="text-muted-foreground hover:text-foreground block truncate font-mono text-xs transition-colors"
            >
              {host}
            </a>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-0.5">
          <span
            title={app.state}
            className={'grid h-5 w-5 shrink-0 place-items-center rounded-full ' + s.ring}
          >
            <span className={'h-1.5 w-1.5 rounded-full ' + s.dot + (s.pulse ? ' pulse-dot' : '')} />
          </span>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button size="icon-sm" variant="ghost" className="text-muted-foreground" title="More">
                <MoreHorizontal />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-44">
              <DropdownMenuItem onClick={() => onLogs(app.name)} className="gap-2">
                <ScrollText className="h-3.5 w-3.5" />
                Logs
              </DropdownMenuItem>
              <DropdownMenuItem onClick={() => onHistory(app.name)} className="gap-2">
                <History className="h-3.5 w-3.5" />
                History
              </DropdownMenuItem>
              {app.origin !== 'cli' && (
                <DropdownMenuItem onClick={() => onEnv(app.name)} className="gap-2">
                  <KeyRound className="h-3.5 w-3.5" />
                  Environment
                </DropdownMenuItem>
              )}
              <DropdownMenuItem asChild className="gap-2">
                <a href={app.url} target="_blank" rel="noreferrer">
                  <ExternalLink className="h-3.5 w-3.5" />
                  Visit
                </a>
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                onClick={() => onStop(app.name)}
                className="gap-2 text-danger focus:text-danger"
              >
                <Square className="h-3.5 w-3.5" />
                Stop
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {app.origin === 'cli' && (
        <span className="text-muted-foreground inline-flex w-fit items-center gap-1.5 rounded-md border border-foreground/8 bg-foreground/2 px-2 py-1 font-mono text-[11px]">
          <Terminal className="h-3 w-3 shrink-0" />
          skiff deploy
        </span>
      )}

      {app.repo && (
        <span className="text-muted-foreground inline-flex w-fit items-center gap-1.5 rounded-md border border-foreground/8 bg-foreground/2 px-2 py-1 font-mono text-[11px]">
          <GitBranch className="h-3 w-3 shrink-0" />
          <span className="truncate">{app.repo}</span>
          {app.auto && (
            <span className="rounded-[3px] border border-success/30 px-1 text-[9px] tracking-wider text-success/90 uppercase">
              auto
            </span>
          )}
        </span>
      )}

      <div className="min-h-[2.25rem] border-t border-foreground/5 pt-3">
        {app.message ? (
          <p className="text-foreground/90 line-clamp-2 text-sm">{app.message}</p>
        ) : (
          <p className="text-muted-foreground text-sm">No deployments yet.</p>
        )}
      </div>

      <div className="text-muted-foreground flex items-center gap-1.5 font-mono text-[11px]">
        {date && <span>{date}</span>}
        {app.branch && (
          <>
            {date && <span className="text-foreground/20">·</span>}
            <GitBranch className="h-3 w-3" />
            <span>{app.branch}</span>
          </>
        )}
      </div>
    </article>
  )
}
