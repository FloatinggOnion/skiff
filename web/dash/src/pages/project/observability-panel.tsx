import { Activity } from 'lucide-react'
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
  ReferenceLine,
} from 'recharts'
import { fmtBytes, fmtMoney } from '@/lib/format'
import { useCost } from '@/hooks/use-cost'
import { UptimeStrip } from './uptime-strip'
import { useInsights } from '@/hooks/use-insights'
import { useResources } from '@/hooks/use-resources'
import { useUrlState } from '@/hooks/use-url-state'
import { ChartGridSkeleton } from '@/components/skeletons'
import { ErrorState } from '@/components/error-state'
import { axisTick, gridStroke, Panel, ChartTip, CpuChart, MemoryChart } from '@/components/charts'
import type { InsightRange, InsightRow, Insights } from '@/services/api.service'

const RANGES: { v: InsightRange; label: string; mins: number }[] = [
  { v: '1h', label: '1h', mins: 60 },
  { v: '24h', label: '24h', mins: 1440 },
  { v: '7d', label: '7d', mins: 1440 },
]

const OK = 'var(--success)'
const WARN = 'var(--warning)'
const BAD = 'var(--danger)'
const LAT = 'var(--chart-violet)'
const MARKER = 'rgba(255,255,255,0.35)'

const tickFor = (range: InsightRange) => (t: number) => {
  const d = new Date(t * 1000)
  return range === '7d'
    ? d.toLocaleDateString([], { weekday: 'short', day: 'numeric' })
    : d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

const spanLabel = (t: number, secs: number, range: InsightRange) => {
  const f = (x: number) =>
    new Date(x * 1000).toLocaleString([], {
      ...(range === '7d' ? { weekday: 'short' } : {}),
      hour: '2-digit',
      minute: '2-digit',
    })
  return `${f(t)} – ${f(t + secs)}`
}

const fmtMs = (ms: number) => (ms >= 1000 ? (ms / 1000).toFixed(1) + 's' : ms + 'ms')

const pct = (n: number, of: number) => (of ? (n / of) * 100 : 0)

export function ObservabilityPanel({ app }: { app: string }) {
  const [rangeParam, setRange] = useUrlState('range', '24h')
  const range = RANGES.find((r) => r.v === rangeParam)?.v ?? '24h'
  const mins = RANGES.find((r) => r.v === range)!.mins
  const { data: a, isPending, isError } = useInsights(app, range)
  const { data: res } = useResources(mins, app)
  const { data: cost } = useCost()
  const costItem = cost?.items.find((i) => i.key === app)
  const monthly =
    cost && cost.price > 0 && costItem ? fmtMoney(costItem.monthly, cost.currency) : null

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between gap-3">
        <p className="text-muted-foreground text-xs">
          Every request the edge served for this project, plus its containers' CPU and memory.
        </p>
        <div className="border-foreground/10 flex shrink-0 rounded-[7px] border p-0.5">
          {RANGES.map((r) => (
            <button
              key={r.v}
              onClick={() => setRange(r.v)}
              className={
                'rounded-[5px] px-2.5 py-1 font-mono text-[11px] transition-colors ' +
                (range === r.v
                  ? 'bg-foreground/10 text-foreground'
                  : 'text-muted-foreground hover:text-foreground')
              }
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>

      <UptimeStrip app={app} />

      {isPending ? (
        <ChartGridSkeleton />
      ) : isError || !a ? (
        <ErrorState message="Couldn't load this project's traffic — retrying…" />
      ) : (
        <>
          <Stats a={a} monthly={monthly} />
          {a.requests === 0 ? (
            <div className="text-muted-foreground border-foreground/8 flex flex-col items-center gap-2 rounded-xl border py-14 text-sm">
              <Activity className="h-5 w-5" />
              No requests in this range yet — traffic shows up here within seconds.
            </div>
          ) : (
            <div className="grid gap-4 lg:grid-cols-2">
              <RequestsChart a={a} range={range} />
              <LatencyChart a={a} range={range} />
            </div>
          )}
        </>
      )}

      {res && res.samples > 0 && (
        <div className="grid gap-4 lg:grid-cols-2">
          <CpuChart r={res} />
          <MemoryChart r={res} />
        </div>
      )}
      {range === '7d' && res && res.samples > 0 && (
        <p className="text-muted-foreground -mt-2 text-[11px]">
          CPU and memory keep the last 24 hours.
        </p>
      )}

      {a && a.requests > 0 && (
        <div className="grid gap-4 lg:grid-cols-2">
          <Table title="Top pages" rows={a.pages} unit="views" />
          <Table title="Referrers" rows={a.referrers} unit="visitors" empty="No referrals yet." />
          <Table
            title="Busiest paths"
            rows={a.paths}
            unit="req"
            extra={(r) => (r.extra != null ? 'avg ' + fmtMs(r.extra) : '')}
          />
          <Table
            title="Errors"
            rows={a.errorPaths}
            unit="times"
            empty="No errors in this range."
            tone="bad"
          />
        </div>
      )}
    </div>
  )
}

function Stats({ a, monthly }: { a: Insights; monthly: string | null }) {
  const errRate = pct(a.errors5, a.requests)
  const tiles = [
    { label: 'Requests', value: a.requests.toLocaleString(), sub: fmtBytes(a.bytes) + ' out' },
    {
      label: 'Server errors',
      value: errRate < 0.1 && a.errors5 > 0 ? '<0.1%' : errRate.toFixed(errRate < 10 ? 1 : 0) + '%',
      sub: `${a.errors5.toLocaleString()} 5xx · ${a.errors4.toLocaleString()} 4xx`,
      tone: errRate >= 1 ? 'text-danger' : '',
    },
    {
      label: 'Response time',
      value: fmtMs(a.p95),
      sub: `p95 · p50 ${fmtMs(a.p50)} · p99 ${fmtMs(a.p99)}`,
    },
    {
      label: 'Visitors',
      value: a.visitors.toLocaleString(),
      sub: `${a.pageviews.toLocaleString()} page views`,
    },
    {
      label: 'Bots & scanners',
      value: pct(a.bots, a.requests).toFixed(0) + '%',
      sub: `${a.bots.toLocaleString()} requests`,
    },
    {
      label: 'Est. cost',
      value: monthly ?? '—',
      sub: monthly ? 'per month, share of the server' : 'set a server price on Server',
    },
  ]
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
      {tiles.map((t) => (
        <div
          key={t.label}
          className="border-foreground/8 from-foreground/2 rounded-xl border bg-linear-to-b to-transparent px-4 py-3.5"
        >
          <p className="text-muted-foreground font-mono text-[10px] tracking-wider uppercase">
            {t.label}
          </p>
          <p
            className={'mt-1.5 text-xl font-semibold tracking-tight tabular-nums ' + (t.tone ?? '')}
          >
            {t.value}
          </p>
          <p className="text-muted-foreground mt-0.5 truncate text-[11px]">{t.sub}</p>
        </div>
      ))}
    </div>
  )
}

function markers(a: Insights) {
  const seen = new Set<number>()
  return a.deploys
    .map((d) => ({ ...d, x: Math.floor(d.t / a.bucketSecs) * a.bucketSecs }))
    .filter((d) => d.x >= a.from && !seen.has(d.x) && seen.add(d.x))
}

function DeployLegend({ a }: { a: Insights }) {
  if (a.deploys.length === 0) return null
  return (
    <span className="text-muted-foreground flex items-center gap-1.5 text-[11px]">
      <span className="bg-foreground/40 h-3 w-px" />
      {a.deploys.length} deploy{a.deploys.length === 1 ? '' : 's'}
    </span>
  )
}

function RequestsChart({ a, range }: { a: Insights; range: InsightRange }) {
  const data = a.series.map((p) => ({ ...p, ok: p.req - p.e4 - p.e5 }))
  const deploys = markers(a)
  return (
    <Panel title="Requests">
      <div className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-1">
        {[
          { label: '2xx/3xx', color: OK },
          { label: '4xx', color: WARN },
          { label: '5xx', color: BAD },
        ].map((s) => (
          <span
            key={s.label}
            className="text-muted-foreground flex items-center gap-1.5 text-[11px]"
          >
            <span className="h-1.5 w-1.5 rounded-full" style={{ background: s.color }} />
            {s.label}
          </span>
        ))}
        <DeployLegend a={a} />
      </div>
      <ResponsiveContainer width="100%" height={170}>
        <BarChart data={data} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
          <CartesianGrid vertical={false} stroke={gridStroke} />
          <XAxis
            dataKey="t"
            tickFormatter={tickFor(range)}
            interval="preserveStartEnd"
            minTickGap={70}
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
                build={(row) => {
                  const dep = deploys.find((d) => d.x === row.t)
                  return {
                    title:
                      spanLabel(row.t, a.bucketSecs, range) +
                      (dep ? ` · deploy ${dep.commit || dep.trigger}` : ''),
                    rows: [
                      { label: '2xx/3xx', color: OK, value: String(row.ok) },
                      { label: '4xx', color: WARN, value: String(row.e4) },
                      { label: '5xx', color: BAD, value: String(row.e5) },
                    ],
                  }
                }}
              />
            }
          />
          <Bar dataKey="ok" stackId="s" fill={OK} maxBarSize={14} isAnimationActive={false} />
          <Bar dataKey="e4" stackId="s" fill={WARN} maxBarSize={14} isAnimationActive={false} />
          <Bar
            dataKey="e5"
            stackId="s"
            fill={BAD}
            maxBarSize={14}
            radius={[2, 2, 0, 0]}
            isAnimationActive={false}
          />
          {deploys.map((d) => (
            <ReferenceLine key={d.id} x={d.x} stroke={MARKER} strokeDasharray="3 3" />
          ))}
        </BarChart>
      </ResponsiveContainer>
    </Panel>
  )
}

function LatencyChart({ a, range }: { a: Insights; range: InsightRange }) {
  const data = a.series.map((p) => ({ ...p, lat: p.req ? p.lat : null }))
  const sparse = data.filter((p) => p.lat != null).length < 4
  const deploys = markers(a)
  return (
    <Panel title="Response time">
      <div className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-1">
        <span className="text-muted-foreground flex items-center gap-1.5 text-[11px]">
          <span className="h-1.5 w-1.5 rounded-full" style={{ background: LAT }} />
          average per {range === '1h' ? 'minute' : range === '24h' ? '15 min' : '2 hours'}
        </span>
        <DeployLegend a={a} />
      </div>
      <ResponsiveContainer width="100%" height={170}>
        <AreaChart data={data} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
          <defs>
            <linearGradient id="lat" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={LAT} stopOpacity={0.25} />
              <stop offset="100%" stopColor={LAT} stopOpacity={0} />
            </linearGradient>
          </defs>
          <CartesianGrid vertical={false} stroke={gridStroke} />
          <XAxis
            dataKey="t"
            tickFormatter={tickFor(range)}
            interval="preserveStartEnd"
            minTickGap={70}
            tick={axisTick}
            tickLine={false}
            axisLine={false}
          />
          <YAxis
            width={44}
            tickFormatter={fmtMs}
            tick={axisTick}
            tickLine={false}
            axisLine={false}
          />
          <Tooltip
            cursor={{ stroke: 'rgba(255,255,255,0.15)' }}
            content={
              <ChartTip
                build={(row) => ({
                  title: spanLabel(row.t, a.bucketSecs, range),
                  rows: [
                    {
                      label: 'Average',
                      color: LAT,
                      value: row.lat == null ? 'no traffic' : fmtMs(row.lat),
                    },
                  ],
                })}
              />
            }
          />
          <Area
            dataKey="lat"
            stroke={LAT}
            strokeWidth={1.5}
            fill="url(#lat)"
            dot={sparse ? { r: 2.5, fill: LAT, strokeWidth: 0 } : false}
            connectNulls
            isAnimationActive={false}
          />
          {deploys.map((d) => (
            <ReferenceLine key={d.id} x={d.x} stroke={MARKER} strokeDasharray="3 3" />
          ))}
        </AreaChart>
      </ResponsiveContainer>
    </Panel>
  )
}

function Table({
  title,
  rows,
  unit,
  extra,
  empty = 'Nothing yet.',
  tone,
}: {
  title: string
  rows: InsightRow[]
  unit: string
  extra?: (r: InsightRow) => string
  empty?: string
  tone?: 'bad'
}) {
  const max = Math.max(1, ...rows.map((r) => r.count))
  return (
    <Panel title={title}>
      {rows.length === 0 ? (
        <p className="text-muted-foreground text-sm">{empty}</p>
      ) : (
        <div className="space-y-1">
          {rows.map((r) => (
            <div key={r.key} className="relative flex items-center gap-3 rounded-[5px] px-2 py-1.5">
              <div
                className={
                  'absolute inset-y-0 left-0 rounded-[5px] ' +
                  (tone === 'bad' ? 'bg-danger/10' : 'bg-foreground/5')
                }
                style={{ width: (r.count / max) * 100 + '%' }}
              />
              <span className="relative min-w-0 flex-1 truncate font-mono text-xs">{r.key}</span>
              {extra && (
                <span className="text-muted-foreground relative shrink-0 font-mono text-[11px]">
                  {extra(r)}
                </span>
              )}
              <span className="relative shrink-0 font-mono text-xs tabular-nums">
                {r.count.toLocaleString()}
                <span className="text-muted-foreground ml-1 text-[10px]">{unit}</span>
              </span>
            </div>
          ))}
        </div>
      )}
    </Panel>
  )
}
