import { useEffect, useState, type FormEvent } from 'react'
import { Box, Database, HardDrive } from 'lucide-react'
import { Link } from 'react-router'
import { fmtBytes, fmtMoney } from '@/lib/format'
import { errText } from '@/lib/errors'
import { useCost, useSetServerPrice } from '@/hooks/use-cost'
import { Button } from '@/components/ui/button'
import type { CostItem, CostReport } from '@/services/api.service'

const KIND_ICON = { app: Box, database: Database, storage: HardDrive }

export function CostSection() {
  const { data: c } = useCost()
  if (!c) return null
  return (
    <section className="mt-6">
      <h2 className="text-muted-foreground mb-3 font-mono text-[11px] tracking-wider uppercase">
        Cost
      </h2>
      <div className="grid gap-4 lg:grid-cols-[1.6fr_1fr]">
        <Breakdown c={c} />
        <div className="flex flex-col gap-4">
          <PriceForm c={c} />
          <Capacity c={c} />
        </div>
      </div>
    </section>
  )
}

function Breakdown({ c }: { c: CostReport }) {
  const money = (v: number) => (c.price > 0 ? fmtMoney(v, c.currency) : '—')
  const rows: { key: string; label: string; sub: string; value: number; item?: CostItem }[] = [
    ...c.items.map((it) => ({
      key: it.key,
      label: it.name,
      sub:
        `${fmtBytes(it.mem)} · ${it.cpu.toFixed(it.cpu < 1 ? 2 : 1)}% CPU` +
        (it.egressGb > 0 ? ` · ${it.egressGb} GB/mo out` : ''),
      value: it.monthly,
      item: it,
    })),
    { key: 'system', label: 'System', sub: 'OS, Docker and Skiff itself', value: c.system },
    { key: 'idle', label: 'Idle headroom', sub: 'capacity nothing is using', value: c.idle },
  ]
  const max = Math.max(0.01, ...rows.map((r) => r.value))
  return (
    <div className="overflow-hidden rounded-xl border border-foreground/8">
      <div className="text-muted-foreground flex items-center justify-between border-b border-foreground/8 px-4 py-2.5 font-mono text-[10px] tracking-wider uppercase">
        <span>Per month, from the last 24h of usage</span>
        <span>{c.price > 0 ? fmtMoney(c.price, c.currency) + ' total' : 'set a price'}</span>
      </div>
      {c.samples === 0 ? (
        <p className="text-muted-foreground p-5 text-sm">
          Collecting usage — numbers appear after a few minutes.
        </p>
      ) : (
        rows.map((r) => {
          const Icon = r.item ? KIND_ICON[r.item.kind] : null
          return (
            <div
              key={r.key}
              className="relative flex items-center gap-3 border-b border-foreground/5 px-4 py-2.5 last:border-0"
            >
              <div
                className={
                  'absolute inset-y-1 left-1 rounded-[5px] ' +
                  (r.item ? 'bg-foreground/5' : 'bg-foreground/[0.025]')
                }
                style={{ width: `calc(${(r.value / max) * 100}% - 0.5rem)` }}
              />
              {Icon ? (
                <Icon className="text-muted-foreground relative h-3.5 w-3.5 shrink-0" />
              ) : (
                <span className="relative w-3.5 shrink-0" />
              )}
              <div className="relative min-w-0 flex-1">
                {r.item?.kind === 'app' ? (
                  <Link to={'/projects/' + r.item.name} className="text-sm hover:underline">
                    {r.label}
                  </Link>
                ) : (
                  <span className={'text-sm ' + (r.item ? '' : 'text-muted-foreground')}>
                    {r.label}
                  </span>
                )}
                <p className="text-muted-foreground truncate font-mono text-[11px]">{r.sub}</p>
              </div>
              <span className="relative shrink-0 font-mono text-sm tabular-nums">
                {money(r.value)}
              </span>
            </div>
          )
        })
      )}
    </div>
  )
}

function PriceForm({ c }: { c: CostReport }) {
  const save = useSetServerPrice()
  const [price, setPrice] = useState('')
  const [currency, setCurrency] = useState(c.currency)
  useEffect(() => {
    setPrice(c.price > 0 ? String(c.price) : '')
    setCurrency(c.currency)
  }, [c.price, c.currency])

  const submit = (e: FormEvent) => {
    e.preventDefault()
    const v = parseFloat(price)
    if (!Number.isNaN(v)) save.mutate({ price: v, currency })
  }

  return (
    <form
      onSubmit={submit}
      className="rounded-xl border border-foreground/8 bg-linear-to-b from-foreground/2 to-transparent p-4"
    >
      <h3 className="text-sm font-medium">Server price</h3>
      <p className="text-muted-foreground mt-1 text-xs">
        What this server costs per month. Each app is charged its share of the box: half its memory,
        half its CPU.
      </p>
      <div className="mt-3 flex items-center gap-2">
        <input
          value={price}
          onChange={(e) => {
            setPrice(e.target.value)
            save.reset()
          }}
          inputMode="decimal"
          placeholder="e.g. 4.99"
          className="h-9 min-w-0 flex-1 rounded-[6px] border border-foreground/12 bg-inset px-3 font-mono text-sm outline-none placeholder:text-foreground/25 focus-visible:border-foreground/30"
        />
        <input
          value={currency}
          onChange={(e) => setCurrency(e.target.value.toUpperCase().slice(0, 3))}
          aria-label="Currency"
          className="h-9 w-16 rounded-[6px] border border-foreground/12 bg-inset px-2 text-center font-mono text-sm outline-none focus-visible:border-foreground/30"
        />
        <Button type="submit" size="sm" loading={save.isPending} disabled={!price.trim()}>
          {save.isSuccess ? 'Saved' : 'Save'}
        </Button>
      </div>
      {save.isError && (
        <p className="mt-2 text-xs text-danger">{errText(save.error, "Couldn't save.")}</p>
      )}
    </form>
  )
}

function Capacity({ c }: { c: CostReport }) {
  const diskFree = c.diskTotal - c.diskUsed
  return (
    <div className="space-y-3 rounded-xl border border-foreground/8 bg-linear-to-b from-foreground/2 to-transparent p-4 text-sm">
      <h3 className="font-medium">Capacity</h3>
      <p className="text-muted-foreground text-xs leading-relaxed">
        {c.medianMem > 0 ? (
          <>
            Free memory fits about{' '}
            <span className="text-foreground font-medium">{c.headroom} more</span> app
            {c.headroom === 1 ? '' : 's'} the size of your typical one ({fmtBytes(c.medianMem)}).
          </>
        ) : (
          <>{fmtBytes(Math.max(0, c.memTotal - c.memUsed))} of memory free.</>
        )}
      </p>
      <p className="text-muted-foreground text-xs leading-relaxed">
        {c.daysToFull >= 0 ? (
          <>
            Disk grows about {fmtBytes(c.diskPerDay)} a day — at that rate the {fmtBytes(diskFree)}{' '}
            left lasts{' '}
            <span
              className={'font-medium ' + (c.daysToFull < 14 ? 'text-danger' : 'text-foreground')}
            >
              ~{c.daysToFull} days
            </span>
            .
          </>
        ) : (
          <>{fmtBytes(diskFree)} of disk free. A fill-up forecast appears after a day of history.</>
        )}
      </p>
    </div>
  )
}
