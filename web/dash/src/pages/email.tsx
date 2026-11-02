import { useMemo, useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { ArrowRight, ChevronDown, Info, Mail, MailCheck, Plus, Trash2, X } from 'lucide-react'
import { useMail } from '@/hooks/use-mail'
import { errText } from '@/lib/errors'
import { useConfirm } from '@/providers/confirm-provider'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import {
  InputGroup,
  InputGroupAddon,
  InputGroupInput,
  InputGroupText,
} from '@/components/ui/input-group'
import { Copyable } from '@/components/copyable'
import { FeedSkeleton } from '@/components/skeletons'
import { ErrorState } from '@/components/error-state'
import type { MailDomain, MailForward } from '@/services/api.service'
import { mailForwardSchema, splitInboxes, type MailForwardInput } from '@/validations/mail'

export default function EmailPage() {
  const { forwards, domains, mxHost, serverIp, relay, isPending, isError, add, remove } = useMail()
  const confirm = useConfirm()
  const [adding, setAdding] = useState(false)

  const byDomain = useMemo(() => {
    const m = new Map<string, MailForward[]>()
    for (const f of forwards) m.set(f.domain, [...(m.get(f.domain) ?? []), f])
    return m
  }, [forwards])

  const receiving = domains.filter((d) => d.mx).length
  const showComposer = !isPending && (adding || (forwards.length === 0 && !isError))

  return (
    <div className="px-8 py-8">
      <header className="mb-6 flex items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Email</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            Forward mail sent to your domains to any inbox — no mail server to run.
          </p>
        </div>
        {forwards.length > 0 && !adding && (
          <Button size="sm" onClick={() => setAdding(true)} className="shrink-0">
            <Plus className="h-4 w-4" />
            Add forward
          </Button>
        )}
      </header>

      {isPending && <FeedSkeleton rows={3} />}
      {!isPending && isError && forwards.length === 0 && (
        <ErrorState message="Couldn't load email forwarding — retrying…" />
      )}

      {showComposer && (
        <AddComposer
          knownDomains={domains.map((d) => d.domain)}
          onAdd={async (v) => {
            await add(v.domain, v.alias, splitInboxes(v.destinations))
            setAdding(false)
          }}
          onCancel={forwards.length > 0 ? () => setAdding(false) : undefined}
        />
      )}

      {domains.length > 0 && (
        <>
          <div className="mt-6 mb-3 flex items-center justify-between gap-3">
            <h2 className="text-muted-foreground font-mono text-[10px] tracking-wider uppercase">
              Domains
            </h2>
            <span className="text-muted-foreground font-mono text-[11px] tabular-nums">
              {receiving}/{domains.length} receiving
            </span>
          </div>
          <div className="flex flex-col gap-3">
            {domains.map((d, i) => (
              <DomainCard
                key={d.domain}
                status={d}
                forwards={byDomain.get(d.domain) ?? []}
                mxHost={mxHost}
                serverIp={serverIp}
                relay={relay}
                delay={i * 40}
                onRemove={async (f) => {
                  const addr = (f.alias === '*' ? 'Catch-all' : f.alias) + '@' + f.domain
                  if (
                    await confirm({
                      title: `Stop forwarding ${addr}?`,
                      description: 'Mail sent to it will be refused.',
                      confirmText: 'Stop forwarding',
                      destructive: true,
                    })
                  )
                    await remove(f.id)
                }}
              />
            ))}
          </div>
          <p className="text-muted-foreground mt-4 flex items-start gap-2 text-xs">
            <Info className="mt-px h-3.5 w-3.5 shrink-0" />
            {relay ? (
              <span>
                Forwarded through your SMTP relay — messages arrive as “Sender via address”, and
                replies go straight back to the sender.
              </span>
            ) : (
              <span>
                Forwarded directly from this server, with the original sender intact. If your
                provider blocks outbound port 25, set{' '}
                <span className="font-mono">SKIFF_SMTP_*</span> to send through a relay instead.
              </span>
            )}
          </p>
        </>
      )}
    </div>
  )
}

function DomainCard({
  status,
  forwards,
  mxHost,
  serverIp,
  relay,
  delay,
  onRemove,
}: {
  status: MailDomain
  forwards: MailForward[]
  mxHost: string
  serverIp: string
  relay: boolean
  delay: number
  onRemove: (f: MailForward) => void
}) {
  const [showDns, setShowDns] = useState(!status.mx)
  const elsewhere = (status.mxHosts ?? []).filter((h) => h !== mxHost)
  const spfValue = `v=spf1 ip4:${serverIp || 'your-server-ip'} ~all`

  return (
    <div
      className="animate-rise overflow-hidden rounded-xl border border-foreground/8"
      style={{ animationDelay: delay + 'ms' }}
    >
      <div className="flex items-center gap-3.5 px-4 py-3.5">
        <span className="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-foreground/10 bg-linear-to-br from-foreground/[0.07] to-transparent">
          <Mail className="h-4 w-4 text-foreground/55" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="truncate font-mono text-sm font-medium">{status.domain}</p>
          <p className="text-muted-foreground mt-0.5 text-xs">
            {forwards.length} {forwards.length === 1 ? 'address' : 'addresses'}
          </p>
        </div>
        {status.mx ? (
          <span className="flex shrink-0 items-center gap-1.5 rounded-full border border-success/20 bg-success/10 px-2.5 py-1 text-[11px] font-medium text-success">
            <MailCheck className="h-3.5 w-3.5" />
            Receiving
          </span>
        ) : (
          <span className="flex shrink-0 items-center gap-1.5 rounded-full border border-warning/20 bg-warning/10 px-2.5 py-1 text-[11px] font-medium text-warning">
            <span className="pulse-dot h-1.5 w-1.5 rounded-full bg-warning" />
            Pending DNS
          </span>
        )}
        <button
          onClick={() => setShowDns((o) => !o)}
          className="text-muted-foreground hover:text-foreground flex shrink-0 items-center gap-1 text-xs transition"
        >
          DNS
          <ChevronDown
            className={'h-3.5 w-3.5 transition-transform ' + (showDns ? 'rotate-180' : '')}
          />
        </button>
      </div>

      {showDns && (
        <div className="animate-rise border-t border-foreground/5 bg-inset px-4 py-4">
          <div className="overflow-hidden rounded-lg border border-foreground/8">
            <div className="text-muted-foreground grid grid-cols-[3.5rem_2.5rem_minmax(0,1fr)_5.5rem] gap-3 border-b border-foreground/8 bg-foreground/2 px-3 py-1.5 font-mono text-[10px] tracking-wider uppercase">
              <span>Type</span>
              <span>Name</span>
              <span>Value</span>
              <span className="text-right">Status</span>
            </div>
            <DnsRow type="MX" value={mxHost} hint="priority 10" ok={status.mx} pending="Missing" />
            {!relay && <DnsRow type="TXT" value={spfValue} ok={status.spf} pending="Recommended" />}
          </div>
          <div className="text-muted-foreground mt-2 space-y-1 text-[11px]">
            {!status.mx && elsewhere.length > 0 && (
              <p className="text-warning/90">
                Mail for this domain currently goes to{' '}
                <span className="font-mono">{elsewhere.join(', ')}</span> — replace those MX records
                to receive it here.
              </p>
            )}
            {!relay && (
              <p>
                Already have an SPF record? Add{' '}
                <span className="text-foreground/70 font-mono">ip4:{serverIp || '…'}</span> to it
                instead of creating a second one.
              </p>
            )}
          </div>
        </div>
      )}

      <div className="divide-y divide-foreground/5 border-t border-foreground/5">
        {forwards.map((f) => (
          <div key={f.id} className="flex items-center gap-3 px-4 py-3">
            <p className="w-48 shrink-0 truncate font-mono text-sm sm:w-64">
              {f.alias === '*' ? (
                <>
                  <span className="text-muted-foreground">*</span>@{f.domain}
                </>
              ) : (
                <>
                  {f.alias}
                  <span className="text-muted-foreground">@{f.domain}</span>
                </>
              )}
            </p>
            <ArrowRight className="text-muted-foreground h-3.5 w-3.5 shrink-0" />
            <div className="flex min-w-0 flex-1 flex-wrap gap-1.5">
              {f.destinations.map((d) => (
                <span
                  key={d}
                  className="text-foreground/80 max-w-full truncate rounded-md border border-foreground/8 bg-foreground/3 px-2 py-0.5 font-mono text-xs"
                >
                  {d}
                </span>
              ))}
            </div>
            {f.alias === '*' && (
              <span className="text-muted-foreground hidden shrink-0 text-[11px] sm:inline">
                Catch-all
              </span>
            )}
            <button
              onClick={() => onRemove(f)}
              title="Stop forwarding"
              className="text-muted-foreground p-1.5 transition hover:text-danger"
            >
              <Trash2 className="h-3.5 w-3.5" />
            </button>
          </div>
        ))}
      </div>
    </div>
  )
}

function DnsRow({
  type,
  value,
  hint,
  ok,
  pending,
}: {
  type: string
  value: string
  hint?: string
  ok: boolean
  pending: string
}) {
  return (
    <div className="grid grid-cols-[3.5rem_2.5rem_minmax(0,1fr)_5.5rem] items-center gap-3 border-b border-foreground/5 px-3 py-2.5 font-mono text-xs last:border-0">
      <span className="text-foreground/90">{type}</span>
      <span className="text-foreground/70">@</span>
      <span className="flex min-w-0 items-center gap-2">
        <Copyable text={value} />
        {hint && <span className="text-muted-foreground shrink-0 text-[10px]">{hint}</span>}
      </span>
      <span className={'text-right text-[11px] ' + (ok ? 'text-success' : 'text-warning')}>
        {ok ? 'Verified' : pending}
      </span>
    </div>
  )
}

function AddComposer({
  knownDomains,
  onAdd,
  onCancel,
}: {
  knownDomains: string[]
  onAdd: (v: MailForwardInput) => Promise<void>
  onCancel?: () => void
}) {
  const [serverError, setServerError] = useState('')
  const form = useForm<MailForwardInput>({
    resolver: zodResolver(mailForwardSchema),
    defaultValues: { alias: '', domain: knownDomains[0] ?? '', destinations: '' },
  })
  const { errors } = form.formState

  const submit = form.handleSubmit(async (v) => {
    setServerError('')
    try {
      await onAdd(v)
      form.reset({ alias: '', domain: v.domain, destinations: '' })
    } catch (err) {
      setServerError(errText(err, 'Could not add that forward.'))
    }
  })

  return (
    <form
      onSubmit={submit}
      className="animate-rise rounded-xl border border-foreground/10 bg-linear-to-b from-foreground/3 to-transparent p-4"
    >
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-sm font-medium">Forward an address</h2>
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
      <FieldGroup className="gap-3">
        <div className="grid gap-3 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1fr)_minmax(0,1.5fr)]">
          <Field data-invalid={!!errors.alias}>
            <FieldLabel htmlFor="mf-alias">Address</FieldLabel>
            <InputGroup>
              <InputGroupInput
                id="mf-alias"
                autoFocus
                className="font-mono"
                placeholder="hello"
                aria-invalid={!!errors.alias}
                {...form.register('alias')}
              />
              <InputGroupAddon align="inline-end">
                <InputGroupText className="font-mono">@</InputGroupText>
              </InputGroupAddon>
            </InputGroup>
            <FieldError errors={[errors.alias]} />
          </Field>
          <Field data-invalid={!!errors.domain}>
            <FieldLabel htmlFor="mf-domain">Domain</FieldLabel>
            <Input
              id="mf-domain"
              list="mf-domains"
              className="font-mono"
              placeholder="yourdomain.com"
              aria-invalid={!!errors.domain}
              {...form.register('domain')}
            />
            <datalist id="mf-domains">
              {knownDomains.map((d) => (
                <option key={d} value={d} />
              ))}
            </datalist>
            <FieldError errors={[errors.domain]} />
          </Field>
          <Field data-invalid={!!errors.destinations}>
            <FieldLabel htmlFor="mf-dest">Forward to</FieldLabel>
            <Input
              id="mf-dest"
              className="font-mono"
              placeholder="you@gmail.com"
              aria-invalid={!!errors.destinations}
              {...form.register('destinations')}
            />
            <FieldError errors={[errors.destinations]} />
          </Field>
        </div>
      </FieldGroup>
      <div className="mt-3 flex flex-wrap items-center justify-between gap-3">
        {serverError ? (
          <p className="text-xs text-danger">{serverError}</p>
        ) : (
          <p className="text-muted-foreground text-xs">
            Use <span className="font-mono">*</span> to catch every address on the domain. Separate
            several inboxes with commas.
          </p>
        )}
        <Button size="sm" type="submit" loading={form.formState.isSubmitting}>
          Add forward
        </Button>
      </div>
    </form>
  )
}
