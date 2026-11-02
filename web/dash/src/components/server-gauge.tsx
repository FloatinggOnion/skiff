import { Link } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { queryKeys } from '@/constants/query-keys'
import { fmtBytes } from '@/lib/format'
import { serverService, type ResourceUsage } from '@/services/api.service'

function Meter({ label, usage, warnAt }: { label: string; usage: ResourceUsage; warnAt: number }) {
  const pct = Math.round((usage.used / usage.total) * 100)
  const tone = pct >= 95 ? 'bg-danger' : pct >= warnAt ? 'bg-warning' : 'bg-seam'
  return (
    <div className="grid gap-1">
      <div className="text-muted-foreground flex justify-between font-mono text-[10px] tabular-nums">
        <span>{label}</span>
        <span>
          {fmtBytes(usage.used)} of {fmtBytes(usage.total)}
        </span>
      </div>
      <div
        role="progressbar"
        aria-label={label + ' used'}
        aria-valuenow={pct}
        className="h-1 overflow-hidden rounded-full bg-foreground/10"
      >
        <div className={'h-full rounded-full ' + tone} style={{ width: Math.min(pct, 100) + '%' }} />
      </div>
    </div>
  )
}

// The one thing a hosted platform can't show you: the machine itself. Owners only; everyone else sees nothing.
export function ServerGauge() {
  const { data } = useQuery({
    queryKey: queryKeys.serverUsage,
    queryFn: () => serverService.usage(),
    refetchInterval: 30000,
    retry: false,
  })
  if (!data || (data.disk.total <= 0 && data.mem.total <= 0)) return null
  return (
    <Link
      to="/server"
      title="Open the Server page"
      className="hover:bg-sidebar-accent grid gap-2 rounded-lg px-2 py-2 transition-colors"
    >
      <span className="text-sidebar-foreground/70 text-xs font-medium">Your server</span>
      {data.disk.total > 0 && <Meter label="Disk" usage={data.disk} warnAt={85} />}
      {data.mem.total > 0 && <Meter label="Memory" usage={data.mem} warnAt={90} />}
    </Link>
  )
}
