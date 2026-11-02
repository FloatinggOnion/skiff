import { useUptime } from '@/hooks/use-insights'
import { relTime } from '@/lib/format'
import type { UptimeDay } from '@/services/api.service'

const fmtPct = (v: number) =>
  v < 0 ? '—' : v >= 99.995 ? '100%' : v.toFixed(v >= 99 ? 2 : 1) + '%'

const barColor = (d: UptimeDay) =>
  d.pct < 0
    ? 'bg-foreground/8'
    : d.pct >= 99.9
      ? 'bg-success/80'
      : d.pct >= 99
        ? 'bg-warning/80'
        : 'bg-danger/80'

const fmtDay = (t: number) =>
  new Date(t * 1000).toLocaleDateString([], { month: 'short', day: 'numeric' })

const fmtSpan = (from: number, to: number) => {
  const mins = Math.max(1, Math.round((to - from) / 60))
  return mins < 60 ? `${mins} min` : `${(mins / 60).toFixed(1)} h`
}

export function UptimeStrip({ app }: { app: string }) {
  const { data: u } = useUptime(app)
  if (!u) return null
  const checking = u.checked > 0

  return (
    <section className="rounded-xl border border-foreground/8 bg-linear-to-b from-foreground/2 to-transparent p-5">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <span
            className={
              'h-2 w-2 rounded-full ' +
              (!checking ? 'bg-foreground/30' : u.up ? 'bg-success' : 'animate-pulse bg-danger')
            }
          />
          <h2 className="text-sm font-semibold">Uptime</h2>
          <span className="text-muted-foreground text-xs">
            {!checking
              ? 'First check runs within a couple of minutes.'
              : u.up
                ? `Up · answered ${u.code} in ${u.ms}ms ${relTime(u.checked)}`
                : `Down · last check ${relTime(u.checked)}`}
          </span>
        </div>
        <div className="text-muted-foreground flex gap-4 font-mono text-[11px]">
          <span>
            24h <span className="text-foreground">{fmtPct(u.pct24h)}</span>
          </span>
          <span>
            7d <span className="text-foreground">{fmtPct(u.pct7d)}</span>
          </span>
          <span>
            30d <span className="text-foreground">{fmtPct(u.pct30d)}</span>
          </span>
        </div>
      </div>
      <div className="flex h-8 items-stretch gap-[3px]">
        {u.days.map((d) => (
          <div
            key={d.day}
            title={`${fmtDay(d.day)} · ${d.pct < 0 ? 'no checks' : fmtPct(d.pct) + ` of ${d.checks} checks`}`}
            className={'min-w-0 flex-1 rounded-[2px] ' + barColor(d)}
          />
        ))}
      </div>
      <div className="text-muted-foreground mt-1.5 flex justify-between font-mono text-[10px]">
        <span>30 days ago</span>
        <span>today</span>
      </div>
      {u.incidents.length > 0 && (
        <div className="mt-4 divide-y divide-foreground/5 border-t border-foreground/5">
          {u.incidents.map((i) => (
            <div key={i.id} className="flex items-center gap-3 py-2 text-xs">
              <span
                className={
                  'h-1.5 w-1.5 shrink-0 rounded-full ' +
                  (i.ended ? 'bg-foreground/30' : 'animate-pulse bg-danger')
                }
              />
              <span className="min-w-0 flex-1 truncate">{i.cause}</span>
              <span className="text-muted-foreground shrink-0 font-mono">
                {i.ended ? `down ${fmtSpan(i.started, i.ended)}` : 'ongoing'} · {relTime(i.started)}
              </span>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
