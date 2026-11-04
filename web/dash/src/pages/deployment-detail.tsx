import { useMemo, useState } from 'react'
import { Link, useParams, useSearchParams } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import { ChevronRight, ExternalLink, RotateCcw, RotateCw, Square } from 'lucide-react'
import { DeployLogPanel } from '@/components/deploy-log-panel'
import { Drawer } from '@/components/drawer'
import { ErrorState } from '@/components/error-state'
import { LogHistory } from '@/components/log-history'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useConsole } from '@/hooks/use-console'
import { useDeployment } from '@/hooks/use-project-deploys'
import { errText } from '@/lib/errors'
import { relTime } from '@/lib/format'
import { useConfirm } from '@/providers/confirm-provider'
import { deploysService, type DeployDetail } from '@/services/api.service'
import { DeployDuration } from './project/deploy-duration'
import { EnvironmentLabel } from './project/deployments-panel'
import { Fact } from './project/fact'
import { deployDot, shortDeployId, statusLabel, triggerLabel } from './project/status'

const RUNTIME_WINDOW = 1800

export default function DeploymentDetailPage() {
  const { name = '', id = '' } = useParams()
  const { data: deploy, isPending, isError } = useDeployment(name, id)

  if (isPending) {
    return (
      <div className="px-4 pt-8 sm:px-8">
        <Skeleton className="mb-4 h-4 w-52" />
        <Skeleton className="mb-6 h-7 w-64" />
        <Skeleton className="h-64 w-full rounded-xl" />
      </div>
    )
  }
  if (isError || !deploy) {
    return (
      <div className="px-4 py-8 sm:px-8">
        <ErrorState message="Couldn't load this deployment. It may belong to a preview that has been removed." />
        <Link
          to={'/projects/' + name + '?tab=deployments'}
          className="text-muted-foreground hover:text-foreground mt-4 block text-center text-sm underline-offset-2 hover:underline"
        >
          Back to deployments
        </Link>
      </div>
    )
  }
  return <Deployment deploy={deploy} />
}

function Deployment({ deploy: d }: { deploy: DeployDetail }) {
  const qc = useQueryClient()
  const confirm = useConfirm()
  const [params] = useSearchParams()
  const [error, setError] = useState('')
  const reload = () => void qc.invalidateQueries({ queryKey: ['deploys', d.app] })
  const term = useConsole(reload)
  const project = d.parent || d.app
  const building = d.status === 'building'
  const canRedeploy = d.current && d.origin !== 'cli'

  // Logs stay tied to this deploy: everything since it for the current one, its first half hour for an older one.
  const [now] = useState(() => Math.floor(Date.now() / 1000))
  const runtime = useMemo(
    () => ({ since: d.started, until: d.current ? now : d.started + RUNTIME_WINDOW }),
    [d.started, d.current, now],
  )

  const stop = async () => {
    setError('')
    try {
      await deploysService.cancel(d.app, d.id)
      reload()
    } catch (e) {
      setError(errText(e, "Couldn't stop this build. Try again."))
    }
  }

  return (
    <div className="flex min-w-0 flex-col px-4 pt-8 pb-16 sm:px-8">
      <nav className="text-muted-foreground mb-4 flex flex-wrap items-center gap-1.5 text-sm">
        <Link to="/" className="hover:text-foreground transition-colors">
          Projects
        </Link>
        <ChevronRight className="h-3.5 w-3.5" />
        <Link to={'/projects/' + project} className="hover:text-foreground transition-colors">
          {project}
        </Link>
        <ChevronRight className="h-3.5 w-3.5" />
        <Link
          to={'/projects/' + project + '?tab=deployments'}
          className="hover:text-foreground transition-colors"
        >
          Deployments
        </Link>
        <ChevronRight className="h-3.5 w-3.5" />
        <span className="text-foreground font-mono font-medium">{shortDeployId(d.id)}</span>
      </nav>

      <header className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-3">
            <h1 className="font-mono text-xl font-semibold tracking-tight">
              {shortDeployId(d.id)}
            </h1>
            <span className="flex items-center gap-1.5 text-sm">
              <span className={'h-2 w-2 shrink-0 rounded-full ' + deployDot(d.status)} />
              {statusLabel(d.status)}
            </span>
            <EnvironmentLabel deploy={d} />
          </div>
          {d.parent && <p className="text-muted-foreground mt-1 font-mono text-xs">{d.app}</p>}
        </div>
        <div className="flex flex-wrap gap-2">
          {building && (
            <Button size="sm" variant="outline" onClick={stop}>
              <Square className="h-3.5 w-3.5 fill-current" />
              Stop build
            </Button>
          )}
          {d.rollbackable && (
            <Button
              size="sm"
              variant="outline"
              onClick={async () => {
                if (
                  await confirm({
                    title: `Roll back ${d.app}?`,
                    description: 'It re-runs this build straight away, with no rebuild.',
                    confirmText: 'Roll back',
                  })
                )
                  term.rollback(d.app, d.id)
              }}
            >
              <RotateCcw className="h-3.5 w-3.5" />
              Roll back to this
            </Button>
          )}
          {canRedeploy && (
            <Button size="sm" variant="outline" onClick={() => term.redeploy(d.app)}>
              <RotateCw className="h-3.5 w-3.5" />
              Redeploy
            </Button>
          )}
        </div>
      </header>
      {error && <p className="text-danger mb-4 text-sm">{error}</p>}

      <dl className="mb-7 grid gap-x-8 gap-y-5 text-sm sm:grid-cols-2 lg:grid-cols-4">
        <Fact label="Duration">
          <span className="font-mono">
            <DeployDuration deploy={d} />
          </span>
        </Fact>
        <Fact label="Source">
          <span className="font-mono">
            {[d.branch, d.commit].filter(Boolean).join(' · ') || '—'}
          </span>
          {d.message && <span className="text-muted-foreground mt-0.5 block">{d.message}</span>}
        </Fact>
        <Fact label="Started">
          {relTime(d.started)}
          <span className="text-muted-foreground block">
            {d.actor ? `${d.actor} · ${triggerLabel(d.trigger)}` : triggerLabel(d.trigger)}
          </span>
        </Fact>
        <Fact label="Address">
          {d.current ? (
            <a
              href={d.url}
              target="_blank"
              rel="noreferrer"
              className="hover:text-foreground inline-flex items-center gap-1 font-mono underline-offset-2 hover:underline"
            >
              {d.url.replace(/^https?:\/\//, '')}
              <ExternalLink className="h-3 w-3 shrink-0 opacity-60" />
            </a>
          ) : building ? (
            <span className="text-muted-foreground">Available once it's ready</span>
          ) : (
            <span className="text-muted-foreground">Not being served</span>
          )}
        </Fact>
      </dl>

      <Tabs
        defaultValue={params.get('tab') === 'runtime' ? 'runtime' : 'build'}
        className="min-w-0"
      >
        <TabsList
          variant="line"
          className="border-foreground/8 mb-4 w-full justify-start gap-6 border-b *:flex-none"
        >
          <TabsTrigger value="build">Build log</TabsTrigger>
          {d.status === 'live' && <TabsTrigger value="runtime">Runtime logs</TabsTrigger>}
        </TabsList>
        <TabsContent value="build">
          <div className="border-foreground/8 bg-inset rounded-xl border p-4">
            <DeployLogPanel app={d.app} id={d.id} showFailure={d.status === 'failed'} />
          </div>
        </TabsContent>
        {d.status === 'live' && (
          <TabsContent value="runtime">
            <p className="text-muted-foreground mb-2 text-xs">
              {d.current
                ? 'Since this deploy went out.'
                : 'The first 30 minutes after this deploy.'}
            </p>
            <div className="border-foreground/8 bg-inset flex h-[60vh] min-h-0 flex-col overflow-hidden rounded-xl border">
              <LogHistory app={d.app} window={runtime} />
            </div>
          </TabsContent>
        )}
      </Tabs>

      {term.stream && <Drawer stream={term.stream} onClose={term.close} onStop={term.stop} />}
    </div>
  )
}
