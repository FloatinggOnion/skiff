import { fmtBytes } from '@/lib/format'
import { Activity, Cpu } from 'lucide-react'
import {
  ResponsiveContainer,
  BarChart,
  Bar,
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
} from 'recharts'
import { useApps } from '@/hooks/use-apps'
import { useAnalytics } from '@/hooks/use-analytics'
import { useResources } from '@/hooks/use-resources'
import { useUrlState } from '@/hooks/use-url-state'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { ChartGridSkeleton } from '@/components/skeletons'
import { ErrorState } from '@/components/error-state'
import type { Analytics, Resources } from '@/services/api.service'
import {
  STATUS,
  axisTick,
  gridStroke,
  fmtClock,
  timeRange,
  fmtPct,
  Panel,
  ChartTip,
  CpuChart,
  MemoryChart,
  CPU_COLOR,
  MEM_COLOR,
} from '@/components/charts'

const RANGES = [
  { v: 60, label: 'Last hour' },
  { v: 360, label: 'Last 6 hours' },
  { v: 720, label: 'Last 12 hours' },
  { v: 1440, label: 'Last 24 hours' },
]

export default function AnalyticsPage() {
  const { apps } = useApps()
  const [rangeParam, setRange] = useUrlState('range', '60')
  const range = RANGES.some((r) => String(r.v) === rangeParam) ? Number(rangeParam) : 60
  const [app, setApp] = useUrlState('app')
  const { data: a, isPending: aPending } = useAnalytics(range, app)
  const { data: r, isPending: rPending } = useResources(range, app)
  const rangeLabel = RANGES.find((r) => r.v === range)?.label ?? 'Last hour'
  const appNames = a?.appOptions ?? r?.appOptions ?? apps.map((x) => x.name)

  return (
    <div className="px-8 py-8">
      <header className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Analytics</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            Traffic and compute for your apps — {rangeLabel.toLowerCase()}.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Select value={app || 'all'} onValueChange={(v) => setApp(v === 'all' ? '' : v)}>
            <SelectTrigger size="sm" className="bg-foreground/2 w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">All apps</SelectItem>
              {appNames.map((n) => (
                <SelectItem key={n} value={n} className="font-mono">
                  {n}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Select value={String(range)} onValueChange={setRange}>
            <SelectTrigger size="sm" className="bg-foreground/2 w-40">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {RANGES.map((r) => (
                <SelectItem key={r.v} value={String(r.v)}>
                  {r.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </header>

      <SectionLabel>Edge traffic</SectionLabel>
      {aPending ? (
        <ChartGridSkeleton count={4} />
      ) : !a ? (
        <ErrorState message="Couldn't load analytics — retrying…" />
      ) : a.total === 0 ? (
        <EmptyState
          icon={<Activity className="h-6 w-6 opacity-40" />}
          label="No traffic in this range."
        />
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          <EdgeRequests a={a} />
          <DataTransfer a={a} />
          <Latency a={a} />
          <TopApps a={a} />
        </div>
      )}

      <div className="mt-10">
        <SectionLabel>Compute</SectionLabel>
        {rPending ? (
          <ChartGridSkeleton count={4} />
        ) : !r ? (
          <ErrorState message="Couldn't load resource metrics — retrying…" />
        ) : r.samples === 0 ? (
          <EmptyState
            icon={<Cpu className="h-6 w-6 opacity-40" />}
            label="No running containers to measure yet."
          />
        ) : (
          <div className="grid gap-4 lg:grid-cols-2">
            <CpuChart r={r} />
            <MemoryChart r={r} />
            <ComputeApps r={r} />
            <ComputeSummary r={r} />
          </div>
        )}
      </div>
    </div>
  )
}

function SectionLabel({ children }: { children: React.ReactNode }) {
  return (
    <h2 className="text-muted-foreground mb-3 font-mono text-[11px] tracking-wide uppercase">
      {children}
    </h2>
  )
}

function EmptyState({ icon, label }: { icon: React.ReactNode; label: string }) {
  return (
    <div className="text-muted-foreground border-foreground/8 flex flex-col items-center gap-2 rounded-xl border py-20 text-sm">
      {icon}
      <span>{label}</span>
    </div>
  )
}

function EdgeRequests({ a }: { a: Analytics }) {
  return (
    <Panel title="Edge Requests">
      <div className="mb-3 flex flex-wrap items-center gap-4">
        <span className="text-2xl font-semibold tracking-tight tabular-nums">
          {a.total.toLocaleString()}
        </span>
        <div className="flex flex-wrap gap-x-3 gap-y-1">
          {STATUS.map((s) => (
            <span
              key={s.key}
              className="text-muted-foreground flex items-center gap-1.5 text-[11px]"
            >
              <span className="h-1.5 w-1.5 rounded-full" style={{ background: s.color }} />
              {s.label} {a.status[s.key].toLocaleString()}
            </span>
          ))}
        </div>
      </div>
      <ResponsiveContainer width="100%" height={150}>
        <BarChart data={a.series} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
          <CartesianGrid vertical={false} stroke={gridStroke} />
          <XAxis
            dataKey="t"
            tickFormatter={fmtClock}
            interval="preserveStartEnd"
            minTickGap={80}
            tick={axisTick}
            tickLine={false}
            axisLine={false}
          />
          <YAxis
            allowDecimals={false}
            width={34}
            tick={axisTick}
            tickLine={false}
            axisLine={false}
          />
          <Tooltip
            cursor={{ fill: 'rgba(255,255,255,0.05)' }}
            content={
              <ChartTip
                build={(row) => ({
                  title: timeRange(row.t, a.bucketSecs),
                  rows: STATUS.map((s) => ({
                    label: s.label,
                    color: s.color,
                    value: String(row[s.key]),
                  })),
                })}
              />
            }
          />
          {STATUS.map((s, i) => (
            <Bar
              key={s.key}
              dataKey={s.key}
              stackId="s"
              fill={s.color}
              maxBarSize={16}
              radius={i === STATUS.length - 1 ? [2, 2, 0, 0] : 0}
              isAnimationActive={false}
            />
          ))}
        </BarChart>
      </ResponsiveContainer>
    </Panel>
  )
}

function DataTransfer({ a }: { a: Analytics }) {
  return (
    <Panel title="Data Transfer">
      <div className="mb-3 flex gap-6">
        <Metric dot="var(--warning)" label="Incoming" value={fmtBytes(a.bytesIn)} />
        <Metric dot="var(--info)" label="Outgoing" value={fmtBytes(a.bytesOut)} />
      </div>
      <ResponsiveContainer width="100%" height={150}>
        <AreaChart data={a.series} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id="bo" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--info)" stopOpacity={0.25} />
              <stop offset="100%" stopColor="var(--info)" stopOpacity={0} />
            </linearGradient>
            <linearGradient id="bi" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--warning)" stopOpacity={0.2} />
              <stop offset="100%" stopColor="var(--warning)" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid vertical={false} stroke={gridStroke} />
          <XAxis
            dataKey="t"
            tickFormatter={fmtClock}
            interval="preserveStartEnd"
            minTickGap={80}
            tick={axisTick}
            tickLine={false}
            axisLine={false}
          />
          <YAxis
            width={44}
            tickFormatter={fmtBytes}
            tick={axisTick}
            tickLine={false}
            axisLine={false}
          />
          <Tooltip
            cursor={{ stroke: 'rgba(255,255,255,0.15)' }}
            content={
              <ChartTip
                build={(row) => ({
                  title: timeRange(row.t, a.bucketSecs),
                  rows: [
                    { label: 'Outgoing', color: 'var(--info)', value: fmtBytes(row.bo) },
                    { label: 'Incoming', color: 'var(--warning)', value: fmtBytes(row.bi) },
                  ],
                })}
              />
            }
          />
          <Area
            dataKey="bo"
            stroke="var(--info)"
            strokeWidth={1.5}
            fill="url(#bo)"
            isAnimationActive={false}
          />
          <Area
            dataKey="bi"
            stroke="var(--warning)"
            strokeWidth={1.5}
            fill="url(#bi)"
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </Panel>
  )
}

function Latency({ a }: { a: Analytics }) {
  return (
    <Panel title="Latency">
      <div className="mb-3 flex items-baseline gap-2">
        <span className="text-2xl font-semibold tracking-tight tabular-nums">{a.avgLatMs}</span>
        <span className="text-muted-foreground text-xs">ms average</span>
      </div>
      <ResponsiveContainer width="100%" height={150}>
        <AreaChart data={a.series} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id="lat" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="var(--chart-violet)" stopOpacity={0.25} />
              <stop offset="100%" stopColor="var(--chart-violet)" stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid vertical={false} stroke={gridStroke} />
          <XAxis
            dataKey="t"
            tickFormatter={fmtClock}
            interval="preserveStartEnd"
            minTickGap={80}
            tick={axisTick}
            tickLine={false}
            axisLine={false}
          />
          <YAxis width={32} tick={axisTick} tickLine={false} axisLine={false} />
          <Tooltip
            cursor={{ stroke: 'rgba(255,255,255,0.15)' }}
            content={
              <ChartTip
                build={(row) => ({
                  title: timeRange(row.t, a.bucketSecs),
                  rows: [
                    { label: 'Avg latency', color: 'var(--chart-violet)', value: row.lat + 'ms' },
                  ],
                })}
              />
            }
          />
          <Area
            dataKey="lat"
            stroke="var(--chart-violet)"
            strokeWidth={1.5}
            fill="url(#lat)"
            isAnimationActive={false}
          />
        </AreaChart>
      </ResponsiveContainer>
    </Panel>
  )
}

function Metric({ dot, label, value }: { dot: string; label: string; value: string }) {
  return (
    <div>
      <p className="text-muted-foreground flex items-center gap-1.5 text-[11px]">
        <span className="h-1.5 w-1.5 rounded-full" style={{ background: dot }} />
        {label}
      </p>
      <p className="mt-0.5 text-lg font-semibold tabular-nums">{value}</p>
    </div>
  )
}

function TopApps({ a }: { a: Analytics }) {
  const max = Math.max(1, ...a.apps.map((x) => x.req))
  return (
    <Panel title="Top apps">
      {a.apps.length === 0 ? (
        <p className="text-muted-foreground text-sm">No app traffic yet.</p>
      ) : (
        <div className="space-y-2.5 pt-1">
          {a.apps.map((app) => (
            <div key={app.name} className="grid grid-cols-[1fr_auto] items-center gap-3">
              <div className="min-w-0">
                <div className="mb-1 flex items-center justify-between gap-2">
                  <span className="truncate font-mono text-sm">{app.name}</span>
                  <span className="text-muted-foreground shrink-0 font-mono text-[11px] tabular-nums">
                    {app.avgLatMs}ms
                  </span>
                </div>
                <div className="bg-foreground/5 h-1.5 overflow-hidden rounded-full">
                  <div
                    className="bg-success/60 h-full rounded-full"
                    style={{ width: (app.req / max) * 100 + '%' }}
                  />
                </div>
              </div>
              <span className="font-mono text-sm tabular-nums">{app.req.toLocaleString()}</span>
            </div>
          ))}
        </div>
      )}
    </Panel>
  )
}

function ComputeApps({ r }: { r: Resources }) {
  const max = Math.max(1, ...r.apps.map((x) => x.mem))
  return (
    <Panel title="Top apps by memory">
      {r.apps.length === 0 ? (
        <p className="text-muted-foreground text-sm">No container usage yet.</p>
      ) : (
        <div className="space-y-2.5 pt-1">
          {r.apps.map((app) => (
            <div key={app.name} className="grid grid-cols-[1fr_auto] items-center gap-3">
              <div className="min-w-0">
                <div className="mb-1 flex items-center justify-between gap-2">
                  <span className="truncate font-mono text-sm">{app.name}</span>
                  <span className="text-muted-foreground shrink-0 font-mono text-[11px] tabular-nums">
                    {fmtPct(app.cpu)} CPU
                  </span>
                </div>
                <div className="bg-foreground/5 h-1.5 overflow-hidden rounded-full">
                  <div
                    className="h-full rounded-full"
                    style={{
                      width: (app.mem / max) * 100 + '%',
                      background: `color-mix(in oklch, ${MEM_COLOR} 60%, transparent)`,
                    }}
                  />
                </div>
              </div>
              <span className="font-mono text-sm tabular-nums">{fmtBytes(app.mem)}</span>
            </div>
          ))}
        </div>
      )}
    </Panel>
  )
}

function ComputeSummary({ r }: { r: Resources }) {
  return (
    <Panel title="Health">
      <div className="grid grid-cols-2 gap-4 pt-1">
        <Metric dot={CPU_COLOR} label="CPU now" value={fmtPct(r.curCpu)} />
        <Metric dot={MEM_COLOR} label="Memory now" value={fmtBytes(r.curMem)} />
        <Metric dot="var(--danger)" label="Restarts" value={r.restarts.toLocaleString()} />
        <Metric dot="var(--chart-violet)" label="Peak memory" value={fmtBytes(r.peakMem)} />
      </div>
      <p className="text-muted-foreground mt-4 text-xs">
        {r.restarts === 0
          ? 'No container restarts in this range — everything held steady.'
          : `${r.restarts} container restart${r.restarts === 1 ? '' : 's'} in this range.`}
      </p>
    </Panel>
  )
}
