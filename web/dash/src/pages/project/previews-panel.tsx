import { useState, type FormEvent } from 'react'
import { ExternalLink, GitBranch, Globe, Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/providers/confirm-provider'
import { useDomains } from '@/hooks/use-domains'
import { errText } from '@/lib/errors'
import { type Preview } from '@/services/api.service'
import { deployDot } from './status'
import { PreviewPassword } from './preview-password'

export function PreviewsPanel({
  project,
  previews,
  passwordProtected,
  onPasswordChange,
  onCreate,
  onTeardown,
}: {
  project: string
  previews: Preview[]
  passwordProtected: boolean
  onPasswordChange: () => void
  onCreate: (branch: string) => void
  onTeardown: (name: string) => void | Promise<void>
}) {
  const { add: addDomain } = useDomains()
  const [branch, setBranch] = useState('')
  const create = (e: FormEvent) => {
    e.preventDefault()
    const b = branch.trim()
    if (!b) return
    onCreate(b)
    setBranch('')
  }
  return (
    <div className="space-y-4">
      <form
        onSubmit={create}
        className="rounded-xl border border-foreground/10 bg-linear-to-b from-foreground/3 to-transparent p-4"
      >
        <h2 className="text-sm font-medium">New preview environment</h2>
        <p className="text-muted-foreground mt-1 mb-3 text-xs">
          Deploy any branch of <span className="text-foreground/70 font-mono">{project}</span> to
          its own live URL with its own certificate. Pushes to the branch redeploy it automatically.
        </p>
        <div className="flex flex-col gap-2 sm:flex-row">
          <div className="relative min-w-0 flex-1">
            <GitBranch className="text-muted-foreground pointer-events-none absolute top-1/2 left-3 h-3.5 w-3.5 -translate-y-1/2" />
            <input
              value={branch}
              onChange={(e) => setBranch(e.target.value)}
              placeholder="branch name — e.g. feat/login"
              className="h-9 w-full rounded-[6px] border border-foreground/12 bg-inset pr-3 pl-9 font-mono text-sm outline-none placeholder:text-foreground/25 focus-visible:border-foreground/30"
            />
          </div>
          <Button type="submit" size="sm" disabled={!branch.trim()} className="shrink-0">
            <Plus className="h-4 w-4" />
            Create preview
          </Button>
        </div>
      </form>

      <PreviewPassword project={project} enabled={passwordProtected} onChanged={onPasswordChange} />

      {previews.length === 0 ? (
        <div className="text-muted-foreground flex flex-col items-center gap-2 rounded-xl border border-foreground/8 py-12 text-sm">
          <GitBranch className="h-5 w-5 opacity-40" />
          <span>No preview environments yet.</span>
        </div>
      ) : (
        <div className="overflow-hidden rounded-xl border border-foreground/8">
          {previews.map((pv) => (
            <PreviewRow
              key={pv.name}
              pv={pv}
              onAddDomain={(host) => addDomain(project, host, pv.branch)}
              onTeardown={() => onTeardown(pv.name)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

function PreviewRow({
  pv,
  onAddDomain,
  onTeardown,
}: {
  pv: Preview
  onAddDomain: (host: string) => Promise<void>
  onTeardown: () => void
}) {
  const confirm = useConfirm()
  const host = pv.url.replace(/^https?:\/\//, '')
  const label = pv.state === 'running' ? 'Ready' : pv.status || pv.state
  const [adding, setAdding] = useState(false)
  const [domain, setDomain] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const h = domain.trim()
    if (!h) return
    setBusy(true)
    setError('')
    try {
      await onAddDomain(h)
      setDomain('')
      setAdding(false)
    } catch (err) {
      setError(errText(err, 'Could not add that domain.'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="border-b border-foreground/5 last:border-0">
      <div className="group flex items-center gap-3.5 px-4 py-3 hover:bg-foreground/2">
        <span className={'h-2 w-2 shrink-0 rounded-full ' + deployDot(pv.status)} />
        <div className="min-w-0 flex-1">
          <a
            href={pv.url}
            target="_blank"
            rel="noreferrer"
            className="hover:text-foreground group/link flex items-center gap-1.5 text-sm font-medium"
          >
            <span className="truncate font-mono">{host}</span>
            <ExternalLink className="h-3 w-3 shrink-0 opacity-0 transition group-hover/link:opacity-60" />
          </a>
          <p className="text-muted-foreground mt-0.5 flex items-center gap-1 truncate text-xs">
            <GitBranch className="h-3 w-3 shrink-0" /> {pv.branch}
          </p>
        </div>
        <span className="text-muted-foreground shrink-0 text-[11px] capitalize">{label}</span>
        <button
          onClick={() => setAdding((a) => !a)}
          title="Add a custom domain for this branch"
          className={
            'hover:text-foreground shrink-0 p-1 transition ' +
            (adding ? 'text-foreground' : 'text-muted-foreground opacity-0 group-hover:opacity-100')
          }
        >
          <Globe className="h-3.5 w-3.5" />
        </button>
        <button
          onClick={async () => {
            if (
              await confirm({
                title: `Tear down ${host}?`,
                description: 'This removes the preview and its build.',
                confirmText: 'Tear down',
                destructive: true,
              })
            )
              onTeardown()
          }}
          title="Tear down preview"
          className="text-muted-foreground shrink-0 p-1 opacity-0 transition group-hover:opacity-100 hover:text-danger"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>

      {adding && (
        <form
          onSubmit={submit}
          className="animate-rise border-t border-foreground/5 bg-inset px-4 py-3 pl-9.5"
        >
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
            <input
              value={domain}
              onChange={(e) => {
                setDomain(e.target.value)
                setError('')
              }}
              autoFocus
              placeholder="staging.yourdomain.com"
              className="h-9 min-w-0 flex-1 rounded-[6px] border border-foreground/12 bg-inset px-3 font-mono text-sm outline-none placeholder:text-foreground/25 focus-visible:border-foreground/30"
            />
            <Button type="submit" size="sm" disabled={busy || !domain.trim()} className="shrink-0">
              Add domain
            </Button>
            <button
              type="button"
              onClick={() => {
                setAdding(false)
                setError('')
              }}
              className="text-muted-foreground hover:text-foreground shrink-0 px-2 text-xs"
            >
              Cancel
            </button>
          </div>
          {error ? (
            <p className="mt-2 text-xs text-danger">{error}</p>
          ) : (
            <p className="text-muted-foreground mt-2 text-xs">
              Binds the hostname to the{' '}
              <span className="text-foreground/70 font-mono">{pv.branch}</span> branch — it follows
              this preview across deploys and gets automatic HTTPS. Point its DNS at your server.
            </p>
          )}
        </form>
      )}
    </div>
  )
}
