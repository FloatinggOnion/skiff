import {
  ChevronRight,
  ExternalLink,
  RotateCcw,
  RotateCw,
  ScrollText,
  Terminal,
} from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { Link, useNavigate, useParams } from 'react-router'
import { ConsoleDrawer } from '@/components/console-drawer'
import { DeployLogPanel } from '@/components/deploy-log-panel'
import { Drawer } from '@/components/drawer'
import { EnvEditor } from '@/components/env-editor'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { FeedSkeleton } from '@/components/skeletons'
import { ErrorState } from '@/components/error-state'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { useConsole } from '@/hooks/use-console'
import { useProject } from '@/hooks/use-project'
import { useAppDeploys } from '@/hooks/use-app-deploys'
import { relTime } from '@/lib/format'
import { useConfirm } from '@/providers/confirm-provider'
import { projectsService, type Deploy } from '@/services/api.service'
import { JobsPanel } from './project/jobs-panel'
import { WorkersPanel } from './project/workers-panel'
import { VolumesPanel } from './project/volumes-panel'
import { RegistryPanel } from './project/registry-panel'
import { DangerZone, SettingsForm } from './project/settings-form'
import { PreviewsPanel } from './project/previews-panel'
import { ObservabilityPanel } from './project/observability-panel'
import { runningPill, deployDot } from './project/status'

export default function ProjectDetailPage() {
  const { name = '' } = useParams()
  const navigate = useNavigate()
  const { project, isPending, reload } = useProject(name)
  const term = useConsole(reload)
  const confirm = useConfirm()
  const [consoleOpen, setConsoleOpen] = useState(false)

  const deployPages = useAppDeploys(name)
  const appDeploys = useMemo(() => {
    const seen = new Set<string>()
    const out: Deploy[] = []
    for (const d of deployPages.data?.pages.flat() ?? []) {
      if (!seen.has(d.id)) {
        seen.add(d.id)
        out.push(d)
      }
    }
    return out
  }, [deployPages.data])

  if (isPending) {
    return (
      <div className="px-8 py-8">
        <Skeleton className="mb-4 h-4 w-40" />
        <div className="mb-6 flex items-center gap-3">
          <Skeleton className="h-11 w-11 rounded-lg" />
          <div className="space-y-2">
            <Skeleton className="h-5 w-40" />
            <Skeleton className="h-3.5 w-56" />
          </div>
        </div>
        <FeedSkeleton rows={4} />
      </div>
    )
  }

  if (!project) {
    return (
      <div className="px-8 py-8">
        <ErrorState message="Couldn't load this project — it may have been removed." />
      </div>
    )
  }

  const latest = project.deploys[0]
  const live = project.deploys.find((d) => d.status === 'live')
  const cli = project.origin === 'cli'
  const fromImage = !!project.image
  const host = project.url.replace(/^https?:\/\//, '')

  return (
    <div className="flex min-h-[calc(100svh-3.5rem)] min-w-0 flex-col px-4 pt-8 sm:px-8">
      {/* breadcrumb */}
      <div className="text-muted-foreground mb-4 flex items-center gap-1.5 text-sm">
        <Link to="/" className="hover:text-foreground transition-colors">
          Projects
        </Link>
        <ChevronRight className="h-3.5 w-3.5" />
        <span className="text-foreground font-medium">{project.name}</span>
      </div>

      {/* header */}
      <header className="mb-6 flex flex-wrap items-start justify-between gap-4">
        <div className="flex items-center gap-3">
          <h1 className="text-xl font-semibold tracking-tight">{project.name}</h1>
          <span
            className={
              'inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 font-mono text-[10px] font-medium tracking-wider uppercase ' +
              runningPill(project.state)
            }
          >
            <span
              className={
                'h-1.5 w-1.5 rounded-full ' +
                (project.state === 'running'
                  ? 'bg-success'
                  : project.state === 'sleeping'
                    ? 'bg-info/70'
                    : 'bg-danger')
              }
            />
            {project.state === 'missing' ? 'not running' : project.state}
          </span>
        </div>
        <div className="flex items-center gap-2">
          <Button size="sm" variant="outline" asChild>
            <Link to={`/logs?app=${encodeURIComponent(project.name)}`}>
              <ScrollText />
              Logs
            </Link>
          </Button>
          <Button
            size="sm"
            variant="outline"
            disabled={project.state !== 'running'}
            title={project.state === 'running' ? 'Open a shell inside the running app' : "The app isn't running"}
            onClick={() => setConsoleOpen(true)}
          >
            <Terminal />
            Console
          </Button>
          {!cli && (
            <Button size="sm" variant="outline" onClick={() => term.redeploy(project.name)}>
              <RotateCw />
              Redeploy
            </Button>
          )}
          {project.state !== 'missing' && (
            <Button size="sm" asChild>
              <a href={project.url} target="_blank" rel="noreferrer">
                <ExternalLink />
                Visit
              </a>
            </Button>
          )}
        </div>
      </header>

      <Tabs defaultValue="overview" className="flex min-w-0 flex-1 flex-col">
        <TabsList
          variant="line"
          className="mb-6 w-full justify-start gap-6 overflow-x-auto border-b border-foreground/8 *:flex-none"
        >
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="deployments">Deployments</TabsTrigger>
          <TabsTrigger value="metrics">Metrics</TabsTrigger>
          <TabsTrigger value="processes">Jobs &amp; workers</TabsTrigger>
          <TabsTrigger value="environment">Environment</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>

        {/* Above the waterline: what's live. Below it: the work that got it there. */}
        <TabsContent value="overview" className="flex min-w-0 flex-1 flex-col">
          <dl className="grid gap-x-8 gap-y-5 pb-7 sm:grid-cols-2 lg:grid-cols-4">
            <Fact label="Address">
              <a
                href={project.url}
                target="_blank"
                rel="noreferrer"
                className="font-mono text-[13px] hover:underline"
              >
                {host}
              </a>
            </Fact>
            <Fact label="Live version">
              {live ? (
                <span className="font-mono text-[13px]">
                  {fromImage ? live.message : live.commit || live.message || 'Deployed'}
                </span>
              ) : (
                <span className="text-muted-foreground text-sm">Nothing live yet</span>
              )}
            </Fact>
            <Fact label="Last deploy">
              {latest ? (
                <span
                  className={
                    'text-sm ' +
                    (latest.status === 'failed'
                      ? 'text-danger'
                      : latest.status === 'live'
                        ? ''
                        : 'text-warning')
                  }
                >
                  {latest.status === 'live' ? 'Succeeded' : capitalize(latest.status)}{' '}
                  <span className="text-muted-foreground">
                    {relTime(latest.started)} · {latest.trigger}
                  </span>
                </span>
              ) : (
                <span className="text-muted-foreground text-sm">None yet</span>
              )}
            </Fact>
            <Fact label="Replicas">
              <span className="text-sm tabular-nums">
                {project.state === 'running' ? project.running : 0} running
                {project.autoscale && (
                  <span className="text-muted-foreground">
                    {' '}
                    · autoscaling {project.scaleMin}–{project.scaleMax}
                  </span>
                )}
              </span>
            </Fact>
            <Fact label="Source">
              {cli ? (
                <span className="font-mono text-[13px]">skiff deploy</span>
              ) : fromImage ? (
                <span className="font-mono text-[13px] break-all">{project.image}</span>
              ) : project.repo ? (
                <a
                  href={'https://github.com/' + project.repo}
                  target="_blank"
                  rel="noreferrer"
                  className="font-mono text-[13px] break-all hover:underline"
                >
                  {project.repo}
                </a>
              ) : (
                <span className="text-muted-foreground text-sm">Git URL</span>
              )}
            </Fact>
            {!cli && !fromImage && (
              <Fact label="Branch">
                <span className="font-mono text-[13px]">
                  {project.branch || 'main'}
                  {project.rootDir ? ' · ' + project.rootDir : ''}
                </span>
              </Fact>
            )}
            {!cli && !fromImage && (
              <Fact label="Auto-deploy">
                <span className="text-sm">{project.auto ? 'On every push' : 'Off'}</span>
              </Fact>
            )}
            <Fact label="Port">
              <span className="font-mono text-[13px]">{project.port || 'From the image'}</span>
            </Fact>
          </dl>
          {latest?.status === 'failed' && (
            <p className="text-danger pb-5 text-sm">
              {live && project.state === 'running'
                ? "The last deploy failed, so the previous version is still serving. The log below says why."
                : "The last deploy failed and nothing is serving. The log below says why."}
            </p>
          )}
          <section className="dark bg-well text-foreground border-seam -mx-4 min-h-[40vh] flex-1 border-t-2 px-4 py-5 sm:-mx-8 sm:px-8">
            <h2 className="text-muted-foreground mb-3 font-mono text-[11px] tracking-wider uppercase">
              {latest ? 'Latest deploy' : 'Build log'}
            </h2>
            {cli ? (
              <p className="text-foreground/60 text-sm">
                This app builds on the machine that runs <span className="font-mono">skiff deploy</span>,
                so its log stays there.
              </p>
            ) : latest ? (
              <DeployLogPanel app={project.name} id={latest.id} />
            ) : (
              <p className="text-foreground/60 text-sm">
                {fromImage ? (
                  'No deploys yet. Redeploy pulls the image and the log appears here.'
                ) : (
                  <>
                    No deploys yet. Push to{' '}
                    <span className="font-mono">{project.branch || 'main'}</span> or hit Redeploy.
                  </>
                )}
              </p>
            )}
          </section>
        </TabsContent>

        {/* Deployments */}
        <TabsContent value="deployments" className="pb-[46vh]">
          <div className="overflow-hidden rounded-xl border border-foreground/8">
            {appDeploys.length === 0 ? (
              <p className="text-muted-foreground p-8 text-center text-sm">No deployments yet.</p>
            ) : (
              appDeploys.map((d) => (
                <div
                  key={d.id}
                  className="group flex items-center gap-2 border-b border-foreground/5 pr-3 transition-colors last:border-0 hover:bg-foreground/[0.03]"
                >
                  <button
                    onClick={() => term.showBuildLog(project.name, d.id)}
                    className="flex min-w-0 flex-1 items-center gap-4 px-5 py-3.5 text-left"
                  >
                    <span className={'h-2 w-2 shrink-0 rounded-full ' + deployDot(d.status)} />
                    <span className="min-w-0 flex-1 truncate text-sm">
                      {d.message || d.commit || d.status}
                    </span>
                    {d.commit && (
                      <span className="text-muted-foreground shrink-0 font-mono text-xs">
                        {d.commit}
                      </span>
                    )}
                    <span className="text-muted-foreground w-16 shrink-0 font-mono text-[11px]">
                      {d.trigger}
                    </span>
                    <span className="text-muted-foreground w-16 shrink-0 text-right font-mono text-[11px]">
                      {relTime(d.started)}
                    </span>
                  </button>
                  {d.status === 'live' && (
                    <Link
                      to={`/logs?app=${encodeURIComponent(project.name)}&since=${d.started}&until=${d.started + 1800}`}
                      title="Runtime logs from the 30 minutes after this deploy"
                      className="text-muted-foreground hover:text-foreground flex shrink-0 items-center gap-1 rounded-[6px] border border-foreground/10 px-2 py-1 text-[11px] opacity-0 transition group-hover:opacity-100 hover:border-foreground/20"
                    >
                      <ScrollText className="h-3 w-3" />
                      Logs
                    </Link>
                  )}
                  {d.rollbackable && (
                    <button
                      onClick={async () => {
                        if (
                          await confirm({
                            title: `Roll back ${project.name}?`,
                            description: 'It re-runs this build instantly — no rebuild.',
                            confirmText: 'Roll back',
                          })
                        )
                          term.rollback(project.name, d.id)
                      }}
                      title="Instant rollback to this build"
                      className="text-muted-foreground hover:text-foreground flex shrink-0 items-center gap-1 rounded-[6px] border border-foreground/10 px-2 py-1 text-[11px] opacity-0 transition group-hover:opacity-100 hover:border-foreground/20"
                    >
                      <RotateCcw className="h-3 w-3" />
                      Rollback
                    </button>
                  )}
                </div>
              ))
            )}
          </div>
          {deployPages.hasNextPage && (
            <div className="mt-4 flex justify-center">
              <Button
                variant="outline"
                size="sm"
                loading={deployPages.isFetchingNextPage}
                onClick={() => deployPages.fetchNextPage()}
              >
                Load more
              </Button>
            </div>
          )}
          {!cli && !fromImage && (
            <section className="mt-10">
              <h2 className="mb-3 text-sm font-semibold">Preview environments</h2>
              <PreviewsPanel
              project={project.name}
              previews={project.previews}
              passwordProtected={project.previewProtected}
              onPasswordChange={reload}
              onCreate={(branch) => term.preview(project.name, branch)}
              onTeardown={async (name) => {
                await projectsService.stop(name)
                reload()
              }}
            />
            </section>
          )}
        </TabsContent>

        <TabsContent value="metrics" className="pb-[46vh]">
          <ObservabilityPanel app={project.name} />
        </TabsContent>

        {/* Environment */}
        <TabsContent value="environment" className="pb-[46vh]">
          {cli && (
            <p className="text-muted-foreground mb-4 text-xs leading-relaxed">
              These apply on the next <span className="font-mono">skiff deploy</span> from a CLI
              signed in with <span className="font-mono">skiff login</span>. Anything its{' '}
              <span className="font-mono">skiff.toml</span> also sets keeps the toml&apos;s value.
            </p>
          )}
          <EnvEditor app={project.name} />
          {!cli && !fromImage && (
            <section className="mt-10 border-t border-foreground/8 pt-6">
              <h2 className="mb-3 text-sm font-semibold">Preview overrides</h2>
              <EnvEditor app={project.name} scope="preview" />
            </section>
          )}
        </TabsContent>

        <TabsContent value="processes" className="flex flex-col gap-10 pb-[46vh]">
          <WorkersPanel app={project.name} />
          <JobsPanel app={project.name} />
        </TabsContent>

        {/* Settings */}
        <TabsContent value="settings" className="flex flex-col gap-10 pb-[46vh]">
          <SettingsForm
            name={project.name}
            branch={project.branch}
            rootDir={project.rootDir}
            port={project.port}
            auto={project.auto}
            previewAuto={project.previewAuto}
            replicas={project.replicas}
            running={project.running}
            release={project.release}
            autoscale={project.autoscale}
            scaleMin={project.scaleMin}
            scaleMax={project.scaleMax}
            scaleCpu={project.scaleCpu}
            sleepAfter={project.sleepAfter}
            healthPath={project.healthPath}
            healthTimeout={project.healthTimeout}
            image={project.image}
            maxReplicas={project.maxReplicas}
            onSaved={reload}
            cli={cli}
          />
          {fromImage && <RegistryPanel app={project.name} />}
          <VolumesPanel app={project.name} onRedeploy={() => term.redeploy(project.name)} />
          <DangerZone name={project.name} onDeleted={() => navigate('/')} />
        </TabsContent>
      </Tabs>

      {consoleOpen && <ConsoleDrawer app={project.name} onClose={() => setConsoleOpen(false)} />}
      {term.stream && <Drawer stream={term.stream} onClose={term.close} onStop={term.stop} />}
    </div>
  )
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-muted-foreground mb-1 font-mono text-[10px] tracking-wider uppercase">
        {label}
      </dt>
      <dd className="break-words">{children}</dd>
    </div>
  )
}

const capitalize = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)
