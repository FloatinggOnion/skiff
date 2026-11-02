import { relTime } from '@/lib/format'
import { GitBranch, History, ScrollText } from 'lucide-react'
import type { SystemInfo } from '@/services/api.service'
import { Button } from '@/components/ui/button'
import { LogoMark } from '@/components/logo'

const deployDot = (status: string): string =>
  status === 'live'
    ? 'bg-success'
    : status === 'failed'
      ? 'bg-danger'
      : 'bg-warning pulse-dot'

// The control plane itself — Skiff deploys itself on push, so its build history
// lives here alongside the apps it runs. Given a distinct luminous treatment.
export function ControlPlaneCard({
  info,
  onHistory,
  onLogs,
}: {
  info: SystemInfo
  onHistory: (app: string) => void
  onLogs: (app: string, id: string) => void
}) {
  const latest = info.deploys[0]
  const building = latest?.status === 'building'
  return (
    <article className="animate-rise group relative flex flex-col gap-3.5 overflow-hidden rounded-xl border border-foreground/15 bg-linear-to-b from-foreground/5 to-foreground/1 p-4 transition-all duration-200 hover:-translate-y-0.5 hover:border-foreground/25 hover:shadow-[0_10px_40px_-15px_rgba(0,0,0,0.9)]">
      <span className="absolute inset-x-0 top-0 h-px bg-linear-to-r from-transparent via-foreground/40 to-transparent" />
      <span className="pointer-events-none absolute -top-16 -left-16 h-40 w-40 rounded-full bg-foreground/6 blur-2xl" />

      <header className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <LogoMark small className="h-4 w-4 shrink-0" />
          <h3 className="truncate text-[15px] font-semibold tracking-tight">Control plane</h3>
        </div>
        <span
          className={
            'inline-flex shrink-0 items-center gap-1.5 rounded-full border px-2 py-0.5 font-mono text-[10px] font-medium tracking-wider uppercase ' +
            (building
              ? 'border-warning/25 bg-warning/10 text-warning'
              : 'border-success/25 bg-success/10 text-success')
          }
        >
          <span
            className={
              'h-1.5 w-1.5 rounded-full ' + (building ? 'pulse-dot bg-warning' : 'bg-success')
            }
          />
          {building ? 'deploying' : 'running'}
        </span>
      </header>

      {info.repo && (
        <div className="text-muted-foreground flex min-w-0 items-center gap-2 text-xs">
          <GitBranch className="h-3.5 w-3.5 shrink-0" />
          <span className="truncate font-mono">{info.repo}</span>
          <span className="shrink-0 rounded border border-foreground/25 px-1.5 py-px font-mono text-[9px] font-medium tracking-wider text-foreground/70 uppercase">
            self-deploys
          </span>
        </div>
      )}

      {latest ? (
        <div className="flex items-center gap-2 font-mono text-xs">
          <span className={'h-1.5 w-1.5 shrink-0 rounded-full ' + deployDot(latest.status)} />
          <span className="text-foreground">{latest.commit || latest.status}</span>
          <span className="text-muted-foreground truncate">
            {latest.trigger} · {relTime(latest.started)}
          </span>
        </div>
      ) : (
        <p className="text-muted-foreground text-xs">
          Rebuilds and hot-swaps itself on every push — zero downtime.
        </p>
      )}

      <div className="mt-auto flex items-center gap-0.5 pt-1">
        <Button size="sm" variant="ghost" onClick={() => onHistory('panel')}>
          <History />
          History
        </Button>
        {latest && (
          <Button size="sm" variant="ghost" onClick={() => onLogs('panel', latest.id)}>
            <ScrollText />
            Logs
          </Button>
        )}
      </div>
    </article>
  )
}
