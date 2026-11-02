import type { ReactNode } from 'react'
import { fmtBytes } from '@/lib/format'
import { errText } from '@/lib/errors'
import { useReclaimSpace, useServer } from '@/hooks/use-server'
import { Button } from '@/components/ui/button'
import { CostSection } from '@/components/cost-section'
import { ControlPlaneBackup } from '@/components/control-plane-backup'
import { Skeleton } from '@/components/ui/skeleton'
import { FeedSkeleton } from '@/components/skeletons'
import { ErrorState } from '@/components/error-state'

function fmtUptime(sec: number): string {
  if (!sec) return '—'
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d) return `${d}d ${h}h`
  if (h) return `${h}h ${m}m`
  return `${m}m`
}

const barColor = (pct: number): string =>
  pct >= 90 ? 'bg-danger' : pct >= 70 ? 'bg-warning' : 'bg-success'

function Meter({
  label,
  big,
  pct,
  sub,
  children,
}: {
  label: string
  big: string
  pct: number
  sub: string
  children?: ReactNode
}) {
  return (
    <section className="rounded-xl border border-foreground/8 bg-linear-to-b from-foreground/2.5 to-transparent p-5">
      <h2 className="text-muted-foreground font-mono text-[11px] tracking-wider uppercase">
        {label}
      </h2>
      <div className="mt-2 flex items-baseline gap-2">
        <span className="text-2xl font-semibold tracking-tight tabular-nums">{big}</span>
        <span className="text-muted-foreground font-mono text-xs">{Math.round(pct)}%</span>
      </div>
      <div className="mt-3 h-1.5 overflow-hidden rounded-full bg-foreground/8">
        <div
          className={'h-full rounded-full transition-all duration-500 ' + barColor(pct)}
          style={{ width: Math.min(100, Math.max(2, pct)) + '%' }}
        />
      </div>
      <p className="text-muted-foreground mt-2 text-xs">{sub}</p>
      {children}
    </section>
  )
}

const Dot = () => <span className="text-foreground/20">·</span>

function ReclaimSpace() {
  const reclaim = useReclaimSpace()
  const r = reclaim.data
  return (
    <div className="mt-3 flex flex-wrap items-center gap-x-3 gap-y-1.5 border-t border-foreground/6 pt-3">
      <Button
        size="sm"
        variant="outline"
        loading={reclaim.isPending}
        onClick={() => reclaim.mutate()}
      >
        Reclaim space
      </Button>
      <span className="text-muted-foreground text-xs">
        {reclaim.isError
          ? errText(reclaim.error)
          : r
            ? `Freed ${fmtBytes(r.freed)}` +
              (r.removed.length
                ? ` · removed ${r.removed.length} unused image${r.removed.length === 1 ? '' : 's'}`
                : '')
            : 'Unused images and old build cache. Runs daily on its own.'}
      </span>
    </div>
  )
}

export default function ServerPage() {
  const { data: s, isPending } = useServer()

  if (isPending) {
    return (
      <div className="px-8 py-8">
        <div className="mb-6 space-y-2">
          <Skeleton className="h-6 w-32" />
          <Skeleton className="h-3.5 w-72" />
        </div>
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="rounded-xl border border-foreground/8 bg-foreground/1.5 p-4">
              <Skeleton className="mb-3 h-3 w-16" />
              <Skeleton className="mb-3 h-6 w-28" />
              <Skeleton className="h-2 w-full" />
            </div>
          ))}
        </div>
        <div className="mt-6">
          <FeedSkeleton rows={4} />
        </div>
      </div>
    )
  }

  if (!s) {
    return (
      <div className="px-8 py-8">
        <ErrorState message="Couldn't read server metrics — retrying…" />
      </div>
    )
  }

  const memPct = s.mem.total ? (s.mem.used / s.mem.total) * 100 : 0
  const diskPct = s.disk.total ? (s.disk.used / s.disk.total) * 100 : 0
  const load = s.load ?? []
  const containers = s.containers ?? []

  return (
    <div className="px-8 py-8">
      <header className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight">Server</h1>
        <p className="text-muted-foreground mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 font-mono text-xs">
          <span className="text-foreground/70">{s.hostname}</span>
          <Dot />
          <span>{s.os}</span>
          {s.docker && (
            <>
              <Dot />
              <span>docker {s.docker}</span>
            </>
          )}
          <Dot />
          <span>up {fmtUptime(s.uptime)}</span>
        </p>
      </header>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
        <Meter
          label="CPU"
          big={Math.round(s.cpuPct) + '%'}
          pct={s.cpuPct}
          sub={`${s.cpuCount} cores${load.length ? ' · load ' + load.map((l) => l.toFixed(2)).join(' ') : ''}`}
        />
        <Meter
          label="Memory"
          big={`${fmtBytes(s.mem.used)} / ${fmtBytes(s.mem.total)}`}
          pct={memPct}
          sub={s.mem.total ? `${fmtBytes(s.mem.total - s.mem.used)} free` : 'unavailable'}
        />
        <Meter
          label="Disk"
          big={`${fmtBytes(s.disk.used)} / ${fmtBytes(s.disk.total)}`}
          pct={diskPct}
          sub={s.disk.total ? `${fmtBytes(s.disk.total - s.disk.used)} free` : 'unavailable'}
        >
          <ReclaimSpace />
        </Meter>
      </div>

      <CostSection />

      <ControlPlaneBackup />

      <section className="mt-6">
        <h2 className="text-muted-foreground mb-3 font-mono text-[11px] tracking-wider uppercase">
          Containers · {containers.length}
        </h2>
        <div className="overflow-hidden rounded-xl border border-foreground/8">
          <div className="text-muted-foreground grid grid-cols-[1.5fr_2fr_0.8fr_1.2fr] gap-3 border-b border-foreground/8 px-4 py-2.5 font-mono text-[10px] tracking-wider uppercase">
            <span>Name</span>
            <span>Image</span>
            <span className="text-right">CPU</span>
            <span className="text-right">Memory</span>
          </div>
          {containers.length === 0 ? (
            <p className="text-muted-foreground p-6 text-center text-sm">No running containers.</p>
          ) : (
            containers.map((c) => (
              <div
                key={c.name}
                className="grid grid-cols-[1.5fr_2fr_0.8fr_1.2fr] items-center gap-3 border-b border-foreground/5 px-4 py-3 text-sm last:border-0"
              >
                <span className="truncate font-mono">{c.name}</span>
                <span className="text-muted-foreground truncate font-mono text-xs">{c.image}</span>
                <span className="text-right font-mono tabular-nums">{c.cpuPct.toFixed(1)}%</span>
                <span className="text-muted-foreground text-right font-mono text-xs tabular-nums">
                  {fmtBytes(c.memUsed)} <span className="text-foreground/25">·</span>{' '}
                  {c.memPct.toFixed(1)}%
                </span>
              </div>
            ))
          )}
        </div>
      </section>
    </div>
  )
}
