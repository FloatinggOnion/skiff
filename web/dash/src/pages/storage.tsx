import { useMemo, useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Link } from 'react-router'
import { Boxes, FolderOpen, Plus, Trash2, X, Copy, Check, Link2, Eye, EyeOff } from 'lucide-react'
import { useApps } from '@/hooks/use-apps'
import { useStorage } from '@/hooks/use-storage'
import { useCopy } from '@/hooks/use-copy'
import { errText } from '@/lib/errors'
import { useConfirm } from '@/providers/confirm-provider'
import { Button } from '@/components/ui/button'
import { CardListSkeleton } from '@/components/skeletons'
import { ErrorState } from '@/components/error-state'
import { Select, SelectContent, SelectItem, SelectTrigger } from '@/components/ui/select'
import type { Bucket } from '@/services/api.service'

export default function StoragePage() {
  const { apps } = useApps()
  const { buckets, isPending, isError, create, remove, attach, detach } = useStorage()
  const confirm = useConfirm()

  const [adding, setAdding] = useState(false)
  const [name, setName] = useState('')

  const createMut = useMutation({
    mutationFn: () => create(name.trim()),
    onSuccess: () => {
      setName('')
      setAdding(false)
    },
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (name.trim()) createMut.mutate()
  }

  return (
    <div className="px-8 py-8">
      <header className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Object storage</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            S3-compatible buckets on your team's private network.
          </p>
        </div>
        {!adding && (
          <Button size="sm" onClick={() => setAdding(true)}>
            <Plus className="h-4 w-4" />
            New bucket
          </Button>
        )}
      </header>

      {adding && (
        <form
          onSubmit={submit}
          className="mb-4 flex flex-wrap items-center gap-2 rounded-xl border border-foreground/8 bg-linear-to-b from-foreground/2 to-transparent p-4"
        >
          <input
            autoFocus
            value={name}
            onChange={(e) => {
              setName(e.target.value)
              createMut.reset()
            }}
            placeholder="bucket name (e.g. uploads)"
            className="h-9 min-w-0 flex-1 rounded-[6px] border border-foreground/12 bg-inset px-3 font-mono text-sm outline-none placeholder:text-foreground/25 focus-visible:border-foreground/30"
          />
          <Button type="submit" size="sm" loading={createMut.isPending} disabled={!name.trim()}>
            Create
          </Button>
          <Button
            type="button"
            size="sm"
            variant="outline"
            onClick={() => {
              setAdding(false)
              createMut.reset()
            }}
          >
            Cancel
          </Button>
          {createMut.isError && (
            <p className="w-full text-xs text-danger">
              {errText(createMut.error, 'Could not create that bucket.')}
            </p>
          )}
        </form>
      )}

      {isPending ? (
        <CardListSkeleton count={2} />
      ) : isError && buckets.length === 0 ? (
        <ErrorState message="Couldn't load your buckets — retrying…" />
      ) : buckets.length === 0 ? (
        <div className="text-muted-foreground flex flex-col items-center gap-2 rounded-xl border border-foreground/8 py-20 text-sm">
          <Boxes className="h-6 w-6 opacity-40" />
          <span>No buckets yet — create one for uploads, backups, or static assets.</span>
        </div>
      ) : (
        <div className="space-y-4">
          {buckets.map((b) => (
            <BucketCard
              key={b.id}
              b={b}
              apps={apps.map((a) => a.name)}
              onDelete={async () => {
                if (
                  await confirm({
                    title: `Delete ${b.name}?`,
                    description: 'The bucket and everything in it are permanently removed.',
                    confirmText: 'Delete',
                    destructive: true,
                  })
                )
                  remove(b.id)
              }}
              onAttach={(app) => attach(b.id, app)}
              onDetach={(app) => detach(b.id, app)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function BucketCard({
  b,
  apps,
  onDelete,
  onAttach,
  onDetach,
}: {
  b: Bucket
  apps: string[]
  onDelete: () => void
  onAttach: (app: string) => void
  onDetach: (app: string) => void
}) {
  const unattached = useMemo(() => apps.filter((a) => !b.attached.includes(a)), [apps, b.attached])
  const running = b.state === 'running'

  return (
    <div className="rounded-xl border border-foreground/8 bg-linear-to-b from-foreground/2 to-transparent p-5">
      <div className="mb-4 flex items-start justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <div className="grid h-9 w-9 place-items-center rounded-lg border border-foreground/8 bg-inset">
            <Boxes className="h-4 w-4 text-seam" />
          </div>
          <div>
            <p className="font-mono text-sm font-medium">{b.name}</p>
            <p className="text-muted-foreground text-[11px]">S3-compatible</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={
              'flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11px] ' +
              (running
                ? 'border-success/20 text-success'
                : 'text-muted-foreground border-foreground/10')
            }
          >
            <span
              className={'h-1.5 w-1.5 rounded-full ' + (running ? 'bg-success' : 'bg-foreground/30')}
            />
            {running ? 'Running' : b.state}
          </span>
          <Button size="sm" variant="outline" asChild disabled={!running}>
            <Link to={'/storage/' + b.id}>
              <FolderOpen className="h-4 w-4" />
              Browse files
            </Link>
          </Button>
          <button
            onClick={onDelete}
            className="text-muted-foreground p-1 hover:text-danger"
            title="Delete bucket"
          >
            <Trash2 className="h-4 w-4" />
          </button>
        </div>
      </div>

      <div className="space-y-4">
        <div className="grid gap-2 sm:grid-cols-2">
          <Detail label="Endpoint" value={b.endpoint} />
          <Detail label="Bucket" value={b.name} />
          <Detail label="Access key" value={b.accessKey} />
          <Detail label="Secret key" value={b.secretKey} secret />
        </div>
        <p className="text-muted-foreground text-[11px]">
          Attaching an app injects <span className="font-mono">S3_ENDPOINT</span>,{' '}
          <span className="font-mono">S3_BUCKET</span>,{' '}
          <span className="font-mono">S3_ACCESS_KEY</span>,{' '}
          <span className="font-mono">S3_SECRET_KEY</span>, and{' '}
          <span className="font-mono">S3_REGION</span> into its environment.
        </p>

        <div>
          <p className="text-muted-foreground mb-1.5 font-mono text-[10px] tracking-wider uppercase">
            Attached apps
          </p>
          <div className="flex flex-wrap items-center gap-2">
            {b.attached.map((app) => (
              <span
                key={app}
                className="flex items-center gap-1.5 rounded-full border border-foreground/12 bg-foreground/2 py-1 pr-1 pl-2.5 font-mono text-xs"
              >
                {app}
                <button
                  onClick={() => onDetach(app)}
                  className="text-muted-foreground hover:text-danger"
                  title={`Detach ${app}`}
                >
                  <X className="h-3 w-3" />
                </button>
              </span>
            ))}
            {unattached.length > 0 && (
              <Select value="" onValueChange={onAttach}>
                <SelectTrigger
                  size="sm"
                  className="text-muted-foreground h-7 w-auto gap-1.5 border-dashed bg-transparent"
                >
                  <Link2 className="h-3.5 w-3.5" />
                  <span>Attach app</span>
                </SelectTrigger>
                <SelectContent>
                  {unattached.map((a) => (
                    <SelectItem key={a} value={a} className="font-mono">
                      {a}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
            {b.attached.length === 0 && unattached.length === 0 && (
              <span className="text-muted-foreground text-xs">
                Deploy an app first, then attach it here.
              </span>
            )}
          </div>
          {b.attached.length > 0 && (
            <p className="text-muted-foreground mt-2 text-[11px]">
              Redeploy an attached app for the S3 credentials to take effect.
            </p>
          )}
        </div>
      </div>
    </div>
  )
}

function Detail({ label, value, secret }: { label: string; value: string; secret?: boolean }) {
  const [show, setShow] = useState(false)
  const { copied, copy } = useCopy()
  const shown = secret && !show ? '•'.repeat(20) : value
  return (
    <div>
      <p className="text-muted-foreground mb-1 font-mono text-[10px] tracking-wider uppercase">
        {label}
      </p>
      <div className="flex items-center gap-2 rounded-[6px] border border-foreground/8 bg-inset px-3 py-2 transition hover:border-foreground/20">
        <span className="text-foreground/80 min-w-0 flex-1 truncate font-mono text-xs">
          {shown}
        </span>
        {secret && (
          <button
            type="button"
            onClick={() => setShow((s) => !s)}
            className="text-muted-foreground hover:text-foreground shrink-0"
            title={show ? 'Hide' : 'Reveal'}
          >
            {show ? <EyeOff className="h-3.5 w-3.5" /> : <Eye className="h-3.5 w-3.5" />}
          </button>
        )}
        <button
          type="button"
          onClick={() => copy(value)}
          className="text-muted-foreground hover:text-foreground shrink-0"
          title="Copy"
        >
          {copied ? (
            <Check className="h-3.5 w-3.5 text-success" />
          ) : (
            <Copy className="h-3.5 w-3.5" />
          )}
        </button>
      </div>
    </div>
  )
}
