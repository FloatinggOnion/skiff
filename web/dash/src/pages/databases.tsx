import { queryKeys } from '@/constants/query-keys'
import { fmtBytes } from '@/lib/format'
import { errText } from '@/lib/errors'
import { useCopy } from '@/hooks/use-copy'
import { useMemo, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Database,
  Plus,
  Trash2,
  X,
  Copy,
  Check,
  TerminalSquare,
  Link2,
  Globe,
  TriangleAlert,
  ShieldCheck,
  Archive,
  Download,
  RotateCcw,
} from 'lucide-react'
import { useApps } from '@/hooks/use-apps'
import { useDatabases } from '@/hooks/use-databases'
import { ConsoleTerminal } from '@/components/console-terminal'
import { UnmanagedDatabases } from '@/components/unmanaged-databases'
import { databasesService, type Backup } from '@/services/api.service'
import { useConfirm } from '@/providers/confirm-provider'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { CardListSkeleton } from '@/components/skeletons'
import { ErrorState } from '@/components/error-state'
import type { Database as Db, DbEngine } from '@/services/api.service'

const ENGINES: { value: DbEngine; label: string; dot: string; accent: string }[] = [
  { value: 'postgres', label: 'PostgreSQL', dot: 'bg-sky-400', accent: 'text-sky-300' },
  { value: 'mysql', label: 'MySQL', dot: 'bg-amber-400', accent: 'text-amber-300' },
  { value: 'mongodb', label: 'MongoDB', dot: 'bg-emerald-400', accent: 'text-emerald-300' },
  { value: 'redis', label: 'Redis', dot: 'bg-rose-400', accent: 'text-rose-300' },
]

const engineMeta = (e: string) => ENGINES.find((x) => x.value === e) ?? ENGINES[0]

export default function DatabasesPage() {
  const { apps } = useApps()
  const { databases, isPending, isError, create, remove, attach, detach, setPublic } =
    useDatabases()
  const confirm = useConfirm()

  const [adding, setAdding] = useState(false)
  const [engine, setEngine] = useState<DbEngine>('postgres')
  const [name, setName] = useState('')
  const createMut = useMutation({
    mutationFn: () => create(engine, name.trim()),
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
      <header className="mb-6 flex items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Databases</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            Managed data stores your apps reach over your team's own private network.
          </p>
        </div>
        {databases.length > 0 && !adding && (
          <Button size="sm" onClick={() => setAdding(true)} className="shrink-0">
            <Plus className="h-4 w-4" />
            New database
          </Button>
        )}
      </header>

      <UnmanagedDatabases dot={(e) => engineMeta(e).dot} />

      {isPending && <CardListSkeleton count={3} />}
      {!isPending && isError && databases.length === 0 && (
        <ErrorState message="Couldn't load databases — retrying…" />
      )}

      {!isPending && (adding || (databases.length === 0 && !isError)) && (
        <form
          onSubmit={submit}
          className="animate-rise rounded-xl border border-white/10 bg-linear-to-b from-white/3 to-transparent p-4"
        >
          <div className="mb-3 flex items-center justify-between">
            <h2 className="text-sm font-medium">New database</h2>
            {databases.length > 0 && (
              <button
                type="button"
                onClick={() => {
                  setAdding(false)
                  setName('')
                  createMut.reset()
                }}
                className="text-muted-foreground hover:text-foreground -mt-1 -mr-1 p-1"
              >
                <X className="h-4 w-4" />
              </button>
            )}
          </div>
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
            <Select value={engine} onValueChange={(v) => setEngine(v as DbEngine)}>
              <SelectTrigger className="shrink-0 cursor-pointer bg-black/30 sm:w-44">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {ENGINES.map((e) => (
                  <SelectItem key={e.value} value={e.value}>
                    <span className="flex items-center gap-2">
                      <span className={'h-1.5 w-1.5 rounded-full ' + e.dot} />
                      {e.label}
                    </span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <input
              value={name}
              onChange={(e) => {
                setName(e.target.value)
                createMut.reset()
              }}
              autoFocus
              placeholder="name (e.g. main)"
              className="h-9 min-w-0 flex-1 rounded-[6px] border border-white/12 bg-black/30 px-3 font-mono text-sm outline-none placeholder:text-white/25 focus-visible:border-white/30"
            />
            <Button
              type="submit"
              size="sm"
              loading={createMut.isPending}
              disabled={!name.trim()}
              className="shrink-0"
            >
              Create
            </Button>
          </div>
          {createMut.isError ? (
            <p className="mt-2 text-xs text-rose-300">
              {errText(createMut.error, 'Could not create that database.')}
            </p>
          ) : (
            <p className="text-muted-foreground mt-2 text-xs">
              Provisions a container with a persistent volume; attach it to an app to inject its
              connection URL.
            </p>
          )}
        </form>
      )}

      {databases.length > 0 && (
        <div className="mt-6 space-y-3">
          {databases.map((db, i) => (
            <DatabaseCard
              key={db.id}
              db={db}
              apps={apps.map((a) => a.name)}
              onAttach={(app) => attach(db.id, app)}
              onDetach={(app) => detach(db.id, app)}
              onSetPublic={setPublic}
              onRemove={async () => {
                if (
                  await confirm({
                    title: `Delete ${db.name}?`,
                    description: 'This destroys its data.',
                    confirmText: 'Delete',
                    destructive: true,
                  })
                )
                  remove(db.id)
              }}
              delay={i * 40}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function DatabaseCard({
  db,
  apps,
  onAttach,
  onDetach,
  onSetPublic,
  onRemove,
  delay,
}: {
  db: Db
  apps: string[]
  onAttach: (app: string) => void
  onDetach: (app: string) => void
  onSetPublic: (id: string, on: boolean) => Promise<void>
  onRemove: () => void
  delay: number
}) {
  const [shell, setShell] = useState(false)
  const [backups, setBackups] = useState(false)
  const [pubBusy, setPubBusy] = useState(false)
  const meta = engineMeta(db.engine)
  const running = db.state === 'running'
  const unattached = useMemo(
    () => apps.filter((a) => !db.attached.includes(a)),
    [apps, db.attached],
  )

  const togglePublic = async () => {
    setPubBusy(true)
    try {
      await onSetPublic(db.id, !db.public)
    } finally {
      setPubBusy(false)
    }
  }

  return (
    <div
      className="animate-rise rounded-xl border border-white/8 bg-linear-to-b from-white/2 to-transparent p-4"
      style={{ animationDelay: delay + 'ms' }}
    >
      <div className="flex items-center gap-3.5">
        <span className="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-white/10 bg-linear-to-br from-white/[0.07] to-transparent">
          <Database className="h-4 w-4 text-white/55" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <p className="truncate font-mono text-sm font-medium">{db.name}</p>
            <span className={'flex items-center gap-1.5 text-[11px] ' + meta.accent}>
              <span className={'h-1.5 w-1.5 rounded-full ' + meta.dot} />
              {meta.label}
            </span>
          </div>
          <p className="text-muted-foreground mt-0.5 truncate font-mono text-xs">
            {db.host}:{db.port}
          </p>
        </div>
        <span
          className={
            'flex shrink-0 items-center gap-1.5 rounded-full border px-2.5 py-1 text-[11px] font-medium ' +
            (running
              ? 'border-emerald-400/20 bg-emerald-400/10 text-emerald-300'
              : 'border-amber-400/20 bg-amber-400/10 text-amber-300')
          }
        >
          <span
            className={
              'h-1.5 w-1.5 rounded-full ' + (running ? 'bg-emerald-400' : 'pulse-dot bg-amber-400')
            }
          />
          {running ? 'Running' : db.state === 'missing' ? 'Stopped' : db.state}
        </span>
        {db.engine !== 'redis' && (
          <button
            onClick={() => setBackups(true)}
            className="text-muted-foreground hover:text-foreground flex shrink-0 items-center gap-1.5 rounded-[6px] border border-white/12 px-2.5 py-1 text-xs transition hover:border-white/25"
          >
            <Archive className="h-3.5 w-3.5" />
            Backups
          </button>
        )}
        <button
          onClick={() => setShell(true)}
          disabled={!running}
          className="text-muted-foreground hover:text-foreground flex shrink-0 items-center gap-1.5 rounded-[6px] border border-white/12 px-2.5 py-1 text-xs transition hover:border-white/25 disabled:opacity-40"
        >
          <TerminalSquare className="h-3.5 w-3.5" />
          Shell
        </button>
        <button
          onClick={onRemove}
          className="text-muted-foreground flex shrink-0 items-center gap-1.5 rounded-[6px] border border-white/12 px-2 py-1 text-xs transition hover:border-rose-500/30 hover:text-rose-300"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>

      <div className="mt-4 space-y-3 border-t border-white/5 pt-3.5 pl-12.5">
        <div>
          <p className="text-muted-foreground mb-1 font-mono text-[10px] tracking-wider uppercase">
            Connection URL
          </p>
          <Copyable text={db.url} />
        </div>

        <div>
          <div className="flex items-center justify-between gap-3">
            <div className="flex items-center gap-2">
              <Globe className="text-muted-foreground h-3.5 w-3.5 shrink-0" />
              <div>
                <p className="text-xs font-medium">Public access</p>
                <p className="text-muted-foreground text-[11px]">
                  Let external clients connect over the internet.
                </p>
              </div>
            </div>
            <Toggle on={db.public} busy={pubBusy} onClick={togglePublic} />
          </div>
          {db.public && (
            <div className="mt-2 space-y-1.5">
              {db.publicUrl ? (
                <Copyable text={db.publicUrl} />
              ) : (
                <p className="text-muted-foreground text-[11px]">
                  Publishing… the public address will appear shortly.
                </p>
              )}
              {db.engine === 'redis' ? (
                <p className="flex items-start gap-1.5 text-[11px] text-amber-300/80">
                  <TriangleAlert className="mt-0.5 h-3 w-3 shrink-0" />
                  Anyone who can reach this address can connect with the password above, and Redis
                  traffic isn't encrypted yet. Use it for trusted networks or temporary access.
                </p>
              ) : (
                <p className="flex items-start gap-1.5 text-[11px] text-emerald-300/80">
                  <ShieldCheck className="mt-0.5 h-3 w-3 shrink-0" />
                  Connections are encrypted with TLS. Anyone who reaches this address can still
                  connect with the password above — keep it safe.
                </p>
              )}
            </div>
          )}
        </div>

        <div>
          <p className="text-muted-foreground mb-1.5 font-mono text-[10px] tracking-wider uppercase">
            Attached apps
          </p>
          <div className="flex flex-wrap items-center gap-2">
            {db.attached.map((app) => (
              <span
                key={app}
                className="flex items-center gap-1.5 rounded-full border border-white/12 bg-white/2 py-1 pr-1 pl-2.5 font-mono text-xs"
              >
                {app}
                <button
                  onClick={() => onDetach(app)}
                  className="text-muted-foreground hover:text-rose-300"
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
            {db.attached.length === 0 && unattached.length === 0 && (
              <span className="text-muted-foreground text-xs">
                Deploy an app first, then attach it here.
              </span>
            )}
          </div>
          {db.attached.length > 0 && (
            <p className="text-muted-foreground mt-2 text-[11px]">
              Redeploy an attached app for the connection URL to take effect.
            </p>
          )}
        </div>
      </div>

      <Dialog open={shell} onOpenChange={setShell}>
        <DialogContent className="sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle className="font-mono text-sm">
              {db.name} · {meta.label} shell
            </DialogTitle>
          </DialogHeader>
          <div className="h-[55vh] overflow-hidden rounded-lg border border-white/10 bg-[#09090b] p-2">
            {shell && <ConsoleTerminal path={`/api/db/exec?db=${encodeURIComponent(db.id)}`} />}
          </div>
        </DialogContent>
      </Dialog>

      <BackupsDialog db={db} open={backups} onOpenChange={setBackups} />
    </div>
  )
}

function fmtDate(t: number) {
  return new Date(t * 1000).toLocaleString([], {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  })
}

function BackupsDialog({
  db,
  open,
  onOpenChange,
}: {
  db: Db
  open: boolean
  onOpenChange: (v: boolean) => void
}) {
  const qc = useQueryClient()
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const confirm = useConfirm()
  const { data: backups = [], isLoading } = useQuery<Backup[]>({
    queryKey: queryKeys.backups(db.id),
    queryFn: () => databasesService.listBackups(db.id),
    enabled: open,
  })
  const reload = () => qc.invalidateQueries({ queryKey: queryKeys.backups(db.id) })

  const run = async (label: string, fn: () => Promise<unknown>) => {
    setError('')
    setBusy(label)
    try {
      await fn()
      reload()
    } catch (err) {
      setError(errText(err, 'Something went wrong.'))
    } finally {
      setBusy('')
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="font-mono text-sm">{db.name} · backups</DialogTitle>
        </DialogHeader>

        <div className="flex items-center justify-between gap-3">
          <p className="text-muted-foreground text-[11px]">
            Automatic daily snapshot on. Keeps the latest {10}.
          </p>
          <Button
            size="sm"
            disabled={busy === 'create'}
            onClick={() => run('create', () => databasesService.createBackup(db.id))}
          >
            {busy === 'create' ? 'Backing up…' : 'Back up now'}
          </Button>
        </div>

        <div className="max-h-[50vh] divide-y divide-white/5 overflow-y-auto rounded-lg border border-white/8">
          {isLoading ? (
            <p className="text-muted-foreground p-5 text-center text-sm">Loading…</p>
          ) : backups.length === 0 ? (
            <p className="text-muted-foreground p-6 text-center text-sm">
              No backups yet. Take one now, or wait for the daily snapshot.
            </p>
          ) : (
            backups.map((b) => (
              <div key={b.id} className="flex items-center gap-3 px-3.5 py-2.5">
                <div className="min-w-0 flex-1">
                  <p className="font-mono text-xs">{fmtDate(b.created)}</p>
                  <p className="text-muted-foreground mt-0.5 text-[11px]">
                    {fmtBytes(b.size)}
                    {b.trigger === 'scheduled' && ' · auto'}
                    {b.offsite === 'ok' && <span className="text-emerald-300/80"> · offsite</span>}
                    {b.offsite === 'pending' && ' · copying offsite…'}
                    {b.offsite === 'failed' && (
                      <span className="text-rose-300"> · offsite copy failed</span>
                    )}
                  </p>
                </div>
                <a
                  href={databasesService.backupDownloadUrl(b.id)}
                  className="text-muted-foreground hover:text-foreground p-1.5"
                  title="Download"
                >
                  <Download className="h-3.5 w-3.5" />
                </a>
                <button
                  onClick={async () => {
                    if (
                      await confirm({
                        title: `Restore ${db.name}?`,
                        description: `This replaces its current contents with the ${fmtDate(b.created)} backup.`,
                        confirmText: 'Restore',
                        destructive: true,
                      })
                    )
                      run('restore-' + b.id, () => databasesService.restoreBackup(b.id))
                  }}
                  disabled={busy === 'restore-' + b.id}
                  className="text-muted-foreground hover:text-foreground p-1.5 disabled:opacity-40"
                  title="Restore"
                >
                  <RotateCcw className="h-3.5 w-3.5" />
                </button>
                <button
                  onClick={() => run('del-' + b.id, () => databasesService.deleteBackup(b.id))}
                  disabled={busy === 'del-' + b.id}
                  className="text-muted-foreground p-1.5 transition hover:text-rose-300 disabled:opacity-40"
                  title="Delete"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            ))
          )}
        </div>
        {error && <p className="text-xs text-rose-300">{error}</p>}
      </DialogContent>
    </Dialog>
  )
}

function Toggle({ on, busy, onClick }: { on: boolean; busy: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      disabled={busy}
      onClick={onClick}
      className={
        'relative h-5 w-9 shrink-0 rounded-full border transition-colors disabled:opacity-50 ' +
        (on ? 'border-emerald-400/30 bg-emerald-400/25' : 'border-white/15 bg-white/8')
      }
    >
      <span
        className={
          'absolute top-0.5 h-3.5 w-3.5 rounded-full bg-white transition-all ' +
          (on ? 'left-4' : 'left-0.5')
        }
      />
    </button>
  )
}

function Copyable({ text }: { text: string }) {
  const { copied, copy } = useCopy()
  return (
    <button
      type="button"
      onClick={() => copy(text)}
      className="group/copy flex w-full items-center gap-2 rounded-[6px] border border-white/8 bg-black/30 px-3 py-2 text-left transition hover:border-white/20"
      title="Copy"
    >
      <span className="text-foreground/80 min-w-0 flex-1 truncate font-mono text-xs">{text}</span>
      {copied ? (
        <Check className="h-3.5 w-3.5 shrink-0 text-emerald-400" />
      ) : (
        <Copy className="text-muted-foreground h-3.5 w-3.5 shrink-0 opacity-50 transition group-hover/copy:opacity-90" />
      )}
    </button>
  )
}
