import { useMemo, useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { errText } from '@/lib/errors'
import {
  Globe,
  Plus,
  Trash2,
  ChevronDown,
  ExternalLink,
  Search,
  ShieldCheck,
  X,
} from 'lucide-react'
import { useApps } from '@/hooks/use-apps'
import { useDomains } from '@/hooks/use-domains'
import { useUrlState } from '@/hooks/use-url-state'
import { useConfirm } from '@/providers/confirm-provider'
import { Button } from '@/components/ui/button'
import { Copyable } from '@/components/copyable'
import { FeedSkeleton } from '@/components/skeletons'
import { ErrorState } from '@/components/error-state'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { App, Domain } from '@/services/api.service'

const targetHost = (apps: App[], app: string): string => {
  const a = apps.find((x) => x.name === app)
  return a ? a.url.replace(/^https?:\/\//, '') : app
}

function dnsRecord(host: string, serverIp: string, appHost: string) {
  const labels = host.split('.')
  if (labels.length <= 2) {
    return { type: 'A', name: '@', value: serverIp || 'your server IP', apex: true }
  }
  return { type: 'CNAME', name: labels[0], value: appHost, apex: false }
}

export default function DomainsPage() {
  const { apps } = useApps()
  const { domains, serverIp, isPending, isError, add, remove } = useDomains()
  const confirm = useConfirm()

  const [query, setQuery] = useUrlState('q')
  const [adding, setAdding] = useState(false)
  const [app, setApp] = useState('')
  const [host, setHost] = useState('')

  const chosenApp = app || apps[0]?.name || ''

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase()
    return q
      ? domains.filter((d) => d.host.includes(q) || d.app.toLowerCase().includes(q))
      : domains
  }, [domains, query])

  const addMut = useMutation({
    mutationFn: () => add(chosenApp, host.trim()),
    onSuccess: () => {
      setHost('')
      setAdding(false)
    },
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (host.trim() && chosenApp) addMut.mutate()
  }

  const active = domains.filter((d) => d.pointsHere).length

  return (
    <div className="px-8 py-8">
      <header className="mb-6 flex items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Domains</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            Serve any app on your own domain, with an automatic HTTPS certificate.
          </p>
        </div>
        {domains.length > 0 && !adding && (
          <Button size="sm" onClick={() => setAdding(true)} className="shrink-0">
            <Plus className="h-4 w-4" />
            Add domain
          </Button>
        )}
      </header>

      {isPending && <FeedSkeleton rows={4} />}
      {!isPending && isError && domains.length === 0 && (
        <ErrorState message="Couldn't load your domains — retrying…" />
      )}

      {!isPending && (adding || (domains.length === 0 && !isError)) && (
        <AddComposer
          apps={apps}
          chosenApp={chosenApp}
          onApp={setApp}
          host={host}
          onHost={(v) => {
            setHost(v)
            addMut.reset()
          }}
          onSubmit={submit}
          onCancel={
            domains.length > 0
              ? () => {
                  setAdding(false)
                  setHost('')
                  addMut.reset()
                }
              : undefined
          }
          busy={addMut.isPending}
          error={addMut.isError ? errText(addMut.error, 'Could not add that domain.') : ''}
        />
      )}

      {domains.length > 0 && (
        <>
          <div className="mt-6 mb-3 flex items-center justify-between gap-3">
            <div className="relative max-w-xs flex-1">
              <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-3 h-3.5 w-3.5 -translate-y-1/2" />
              <input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder="Search domains…"
                className="border-foreground/12 bg-foreground/2 placeholder:text-foreground/30 focus-visible:border-foreground/25 h-9 w-full rounded-[6px] border pr-3 pl-8.5 text-sm outline-none"
              />
            </div>
            <span className="text-muted-foreground shrink-0 font-mono text-[11px] tabular-nums">
              {active}/{domains.length} active
            </span>
          </div>

          <div className="border-foreground/8 overflow-hidden rounded-xl border">
            {filtered.length === 0 ? (
              <p className="text-muted-foreground p-8 text-center text-sm">
                No domains match “{query}”.
              </p>
            ) : (
              filtered.map((d, i) => (
                <DomainRow
                  key={d.host}
                  domain={d}
                  serverIp={serverIp}
                  appHost={targetHost(apps, d.app)}
                  onRemove={async () => {
                    if (
                      await confirm({
                        title: `Remove ${d.host}?`,
                        confirmText: 'Remove',
                        destructive: true,
                      })
                    )
                      remove(d.host)
                  }}
                  delay={i * 40}
                />
              ))
            )}
          </div>
        </>
      )}
    </div>
  )
}

function DomainRow({
  domain,
  serverIp,
  appHost,
  onRemove,
  delay,
}: {
  domain: Domain
  serverIp: string
  appHost: string
  onRemove: () => void
  delay: number
}) {
  const [open, setOpen] = useState(false)
  const rec = dnsRecord(domain.host, serverIp, appHost)
  const resolves = domain.resolvesTo ?? []

  return (
    <div
      className="animate-rise border-foreground/5 border-b last:border-0"
      style={{ animationDelay: delay + 'ms' }}
    >
      {/* header (click to expand) */}
      <button
        onClick={() => setOpen((o) => !o)}
        className="group hover:bg-foreground/2 flex w-full items-center gap-3.5 px-4 py-3.5 text-left transition-colors"
      >
        <span className="border-foreground/10 from-foreground/[0.07] grid h-9 w-9 shrink-0 place-items-center rounded-md border bg-linear-to-br to-transparent">
          <Globe className="text-foreground/55 h-4 w-4" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="truncate font-mono text-sm font-medium">{domain.host}</p>
          <p className="text-muted-foreground mt-0.5 truncate text-xs">
            {domain.branch ? (
              <>
                <span className="text-foreground/70 font-mono">{domain.branch}</span> branch of{' '}
                <span className="text-foreground/70">{domain.parent}</span>
              </>
            ) : (
              <>
                Connected to <span className="text-foreground/70">{domain.app}</span>
              </>
            )}
          </p>
        </div>
        {domain.pointsHere ? (
          <span className="border-success/20 bg-success/10 text-success flex shrink-0 items-center gap-1.5 rounded-full border px-2.5 py-1 text-[11px] font-medium">
            <ShieldCheck className="h-3.5 w-3.5" />
            Active
          </span>
        ) : (
          <span className="border-warning/20 bg-warning/10 text-warning flex shrink-0 items-center gap-1.5 rounded-full border px-2.5 py-1 text-[11px] font-medium">
            <span className="pulse-dot bg-warning h-1.5 w-1.5 rounded-full" />
            Pending DNS
          </span>
        )}
        <ChevronDown
          className={
            'text-muted-foreground h-4 w-4 shrink-0 transition-transform ' +
            (open ? 'rotate-180' : '')
          }
        />
      </button>

      {/* expanded detail */}
      {open && (
        <div className="animate-rise border-foreground/5 bg-inset space-y-4 border-t px-4 py-4 pl-13.5">
          {/* DNS record */}
          <section>
            <h3 className="text-muted-foreground mb-2 font-mono text-[10px] tracking-wider uppercase">
              DNS record
            </h3>
            <div className="border-foreground/8 overflow-hidden rounded-lg border">
              <div className="text-muted-foreground border-foreground/8 bg-foreground/2 grid grid-cols-[4rem_1fr_1.6fr] gap-2 border-b px-3 py-1.5 font-mono text-[10px] tracking-wider uppercase">
                <span>Type</span>
                <span>Name</span>
                <span>Value</span>
              </div>
              <div className="grid grid-cols-[4rem_1fr_1.6fr] items-center gap-2 px-3 py-2.5 font-mono text-xs">
                <span className="text-foreground/90">{rec.type}</span>
                <span className="text-foreground/70">{rec.name}</span>
                <Copyable text={rec.value} />
              </div>
            </div>
            <p className="text-muted-foreground mt-2 text-[11px]">
              {rec.apex
                ? 'Apex domains need an A record (a CNAME is not allowed at the root).'
                : `Or an A record → ${serverIp || 'your server IP'}.`}
            </p>
          </section>

          {/* status */}
          <section className="flex flex-wrap items-start gap-x-6 gap-y-3">
            <Detail label="Status">
              {domain.pointsHere ? (
                <span className="text-success">DNS points here</span>
              ) : (
                <span className="text-warning">
                  Awaiting DNS
                  {resolves.length > 0 && (
                    <span className="text-muted-foreground">
                      {' '}
                      — resolves to <span className="font-mono">{resolves.join(', ')}</span>,
                      expected{' '}
                      <span className="text-foreground/70 font-mono">{serverIp || '—'}</span>
                    </span>
                  )}
                </span>
              )}
            </Detail>
            <Detail label="Certificate">
              <span className="flex items-center gap-1.5">
                {domain.pointsHere ? (
                  <ShieldCheck className="text-success h-3.5 w-3.5" />
                ) : (
                  <ShieldCheck className="text-muted-foreground h-3.5 w-3.5" />
                )}
                <span className="text-foreground/70">Automatic — Let's Encrypt</span>
              </span>
            </Detail>
          </section>

          {/* actions */}
          <div className="flex items-center gap-2 pt-1">
            <a
              href={'https://' + domain.host}
              target="_blank"
              rel="noreferrer"
              className="hover:text-foreground text-muted-foreground border-foreground/12 hover:border-foreground/25 flex items-center gap-1.5 rounded-[6px] border px-2.5 py-1 text-xs transition"
            >
              <ExternalLink className="h-3.5 w-3.5" />
              Visit
            </a>
            <button
              onClick={onRemove}
              className="text-muted-foreground border-foreground/12 hover:border-danger/30 hover:text-danger flex items-center gap-1.5 rounded-[6px] border px-2.5 py-1 text-xs transition"
            >
              <Trash2 className="h-3.5 w-3.5" />
              Remove
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

function Detail({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <p className="text-muted-foreground mb-1 font-mono text-[10px] tracking-wider uppercase">
        {label}
      </p>
      <div className="text-xs">{children}</div>
    </div>
  )
}

function AddComposer({
  apps,
  chosenApp,
  onApp,
  host,
  onHost,
  onSubmit,
  onCancel,
  busy,
  error,
}: {
  apps: App[]
  chosenApp: string
  onApp: (v: string) => void
  host: string
  onHost: (v: string) => void
  onSubmit: (e: FormEvent) => void
  onCancel?: () => void
  busy: boolean
  error: string
}) {
  return (
    <form
      onSubmit={onSubmit}
      className="animate-rise border-foreground/10 from-foreground/3 rounded-xl border bg-linear-to-b to-transparent p-4"
    >
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-medium">Add a domain</h2>
        {onCancel && (
          <button
            type="button"
            onClick={onCancel}
            className="text-muted-foreground hover:text-foreground -mt-1 -mr-1 p-1"
          >
            <X className="h-4 w-4" />
          </button>
        )}
      </div>
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
        <input
          value={host}
          onChange={(e) => onHost(e.target.value)}
          autoFocus
          placeholder="www.yourdomain.com"
          className="border-foreground/12 bg-inset placeholder:text-foreground/25 focus-visible:border-foreground/30 h-9 min-w-0 flex-1 rounded-[6px] border px-3 font-mono text-sm outline-none"
        />
        <span className="text-muted-foreground hidden shrink-0 text-xs sm:block">→</span>
        <Select value={chosenApp} onValueChange={onApp} disabled={apps.length === 0}>
          <SelectTrigger className="bg-inset shrink-0 cursor-pointer sm:w-44">
            <SelectValue placeholder="Select app" />
          </SelectTrigger>
          <SelectContent>
            {apps.map((a) => (
              <SelectItem key={a.name} value={a.name} className="font-mono">
                {a.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Button
          type="submit"
          size="sm"
          loading={busy}
          disabled={!host.trim() || !chosenApp}
          className="shrink-0"
        >
          Add
        </Button>
      </div>
      {error ? (
        <p className="text-danger mt-2 text-xs">{error}</p>
      ) : (
        <p className="text-muted-foreground mt-2 text-xs">
          You'll point DNS at your server; the HTTPS certificate is issued automatically.
        </p>
      )}
    </form>
  )
}
