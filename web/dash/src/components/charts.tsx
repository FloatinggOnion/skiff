import type { ReactNode } from 'react'
import {
  ResponsiveContainer,
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
} from 'recharts'
import { fmtBytes } from '@/lib/format'
import type { Resources } from '@/services/api.service'

export const STATUS = [
  { key: 's2' as const, label: '2XX', color: 'var(--success)' },
  { key: 's3' as const, label: '3XX', color: 'var(--info)' },
  { key: 's4' as const, label: '4XX', color: 'var(--warning)' },
  { key: 's5' as const, label: '5XX', color: 'var(--danger)' },
]

export const axisTick = { fill: 'var(--muted-foreground)', fontSize: 10, fontFamily: 'ui-monospace, monospace' }
export const gridStroke = 'rgba(255,255,255,0.05)'

export const fmtClock = (t: number) =>
  new Date(t * 1000).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })

export const timeRange = (t: number, secs: number) => `${fmtClock(t)} – ${fmtClock(t + secs)}`

// Small CPU values are real signal, not zero — keep two decimals under 1% so a
// lightly-used app reads "0.12%" instead of a flat, misleading "0.0%".
export const fmtPct = (v: number) => {
  if (v <= 0) return '0%'
  if (v >= 100) return Math.round(v) + '%'
  if (v < 1) return v.toFixed(2) + '%'
  return v.toFixed(1) + '%'
}

export const CPU_COLOR = 'var(--seam)' // orange-400
export const MEM_COLOR = 'var(--chart-teal)' // teal-400

export function Panel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="rounded-xl border border-foreground/8 bg-linear-to-b from-foreground/2 to-transparent p-5">
      <h2 className="mb-3 text-sm font-semibold">{title}</h2>
      {children}
    </section>
  )
}

export function TipCard({
  title,
  rows,
}: {
  title: string
  rows: { label: string; color: string; value: string }[]
}) {
  return (
    <div className="rounded-lg border border-foreground/10 bg-inset px-3 py-2 shadow-xl backdrop-blur-sm">
      <p className="text-muted-foreground mb-1.5 font-mono text-[10px]">{title}</p>
      <div className="space-y-1">
        {rows.map((r) => (
          <div key={r.label} className="flex items-center justify-between gap-6 text-xs">
            <span className="flex items-center gap-1.5">
              <span className="h-2 w-2 rounded-full" style={{ background: r.color }} />
              {r.label}
            </span>
            <span className="font-mono tabular-nums">{r.value}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

export type Row = Record<string, number>

export function ChartTip(props: {
  active?: boolean
  payload?: { payload: Row }[]
  build?: (row: Row) => { title: string; rows: { label: string; color: string; value: string }[] }
}) {
  const { active, payload, build } = props
  if (!active || !payload?.length || !build) return null
  const { title, rows } = build(payload[0].payload)
  return <TipCard title={title} rows={rows} />
}

export function ResourceChart({
  r,
  color,
  gradId,
  dataKey,
  fmt,
  tipLabel,
  yWidth,
}: {
  r: Resources
  color: string
  gradId: string
  dataKey: 'cpu' | 'mem'
  fmt: (v: number) => string
  tipLabel: string
  yWidth: number
}) {
  return (
    <ResponsiveContainer width="100%" height={150}>
      <AreaChart data={r.series} margin={{ top: 4, right: 4, bottom: 0, left: 0 }}>
        <defs>
          <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity={0.25} />
            <stop offset="100%" stopColor={color} stopOpacity={0} />
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
          width={yWidth}
          tickFormatter={fmt}
          tick={axisTick}
          tickLine={false}
          axisLine={false}
        />
        <Tooltip
          cursor={{ stroke: 'rgba(255,255,255,0.15)' }}
          content={
            <ChartTip
              build={(row) => {
                const v = row[dataKey] as number | null
                return {
                  title: timeRange(row.t, r.bucketSecs),
                  rows: [{ label: tipLabel, color, value: v == null ? 'no data' : fmt(v) }],
                }
              }}
            />
          }
        />
        <Area
          dataKey={dataKey}
          stroke={color}
          strokeWidth={1.5}
          fill={`url(#${gradId})`}
          isAnimationActive={false}
        />
      </AreaChart>
    </ResponsiveContainer>
  )
}

export function CpuChart({ r }: { r: Resources }) {
  return (
    <Panel title="CPU">
      <div className="mb-3 flex items-baseline gap-2">
        <span className="text-2xl font-semibold tracking-tight tabular-nums">
          {fmtPct(r.curCpu)}
        </span>
        <span className="text-muted-foreground text-xs">now · peak {fmtPct(r.peakCpu)}</span>
      </div>
      <ResourceChart
        r={r}
        color={CPU_COLOR}
        gradId="cpu"
        dataKey="cpu"
        fmt={fmtPct}
        tipLabel="CPU"
        yWidth={40}
      />
    </Panel>
  )
}

export function MemoryChart({ r }: { r: Resources }) {
  return (
    <Panel title="Memory">
      <div className="mb-3 flex items-baseline gap-2">
        <span className="text-2xl font-semibold tracking-tight tabular-nums">
          {fmtBytes(r.curMem)}
        </span>
        <span className="text-muted-foreground text-xs">now · peak {fmtBytes(r.peakMem)}</span>
      </div>
      <ResourceChart
        r={r}
        color={MEM_COLOR}
        gradId="mem"
        dataKey="mem"
        fmt={fmtBytes}
        tipLabel="Memory"
        yWidth={56}
      />
    </Panel>
  )
}
