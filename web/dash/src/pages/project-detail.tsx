import { ChevronRight, ExternalLink, RotateCw, ScrollText, Terminal } from 'lucide-react'
import { useState } from 'react'
import { Link, useNavigate, useParams, useSearchParams } from 'react-router'
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
import { relTime } from '@/lib/format'
import { projectsService } from '@/services/api.service'
import { JobsPanel } from './project/jobs-panel'
import { WorkersPanel } from './project/workers-panel'
import { TCPPortSection } from './project/tcp-port-section'
import { VolumesPanel } from './project/volumes-panel'
import { RegistryPanel } from './project/registry-panel'
import { DangerZone, SettingsForm } from './project/settings-form'
import { PreviewsPanel } from './project/previews-panel'
import { DeploymentsPanel } from './project/deployments-panel'
import { Fact } from './project/fact'
import { ObservabilityPanel } from './project/observability-panel'
import { runningPill } from './project/status'

export default function ProjectDetailPage() {
  const { name = '' } = useParams()
  const [searchParams] = useSearchParams()
  const navigate = useNavigate()
  const { project, isPending, reload } = useProject(name)
  const term = useConsole(reload)
  const [consoleOpen, setConsoleOpen] = useState(false)

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
  const tab = searchParams.get('tab') ?? 'overview'
  const fromImage = !!project.image
  const skiffHost = project.url.replace(/^https?:\/\//, '')
  const primary = project.domains[0]
  const visitUrl = primary ? 'https://' + primary : project.url

  return (
    <div className="flex min-h-[calc(100svh-3.5rem)] min-w-0 flex-col px-4 pt-8 sm:px-8">
      {/* breadcrumb */}
      <div className="text-muted-foreground mb-4 flex items-center gap-1.5 text-sm">
        <Link to="/" className="hover:text-foreground transition-colors">
          Projects
        </Link>
        <ChevronRight className="h-3.5 w-3.5" />
        {project.parent && (
          <>
            <Link
              to={'/projects/' + project.parent + '?tab=previews'}
              className="hover:text-foreground transition-colors"
            >
              {project.parent}
            </Link>
            <ChevronRight className="h-3.5 w-3.5" />
          </>
        )}
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
            title={
              project.state === 'running'
                ? 'Open a shell inside the running app'
                : "The app isn't running"
            }
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
          {project.state !== 'missing' && !project.private && (
            <Button size="sm" asChild>
              <a href={visitUrl} target="_blank" rel="noreferrer">
                <ExternalLink />
                Visit
              </a>
            </Button>
          )}
        </div>
      </header>

      <Tabs defaultValue={tab} key={project.name} className="flex min-w-0 flex-1 flex-col">
        <TabsList
          variant="line"
          className="border-foreground/8 mb-6 w-full justify-start gap-6 overflow-x-auto border-b *:flex-none"
        >
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="deployments">Deployments</TabsTrigger>
          {!cli && !fromImage && !project.parent && (
            <TabsTrigger value="previews">
              Previews
              {project.previews.length > 0 && (
                <span className="text-muted-foreground ml-1.5 font-mono text-[11px]">
                  {project.previews.length}
                </span>
              )}
            </TabsTrigger>
          )}
          <TabsTrigger value="metrics">Metrics</TabsTrigger>
          <TabsTrigger value="processes">Jobs &amp; workers</TabsTrigger>
          <TabsTrigger value="environment">Environment</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>

        {/* Above the waterline: what's live. Below it: the work that got it there. */}
        <TabsContent value="overview" className="flex min-w-0 flex-1 flex-col">
          <dl className="grid gap-x-8 gap-y-5 pb-7 sm:grid-cols-2 lg:grid-cols-4">
            {project.domains.length > 0 && !project.private && (
              <Fact label={project.domains.length > 1 ? 'Custom domains' : 'Custom domain'}>
                <div className="flex flex-col gap-0.5">
                  {project.domains.map((d) => (
                    <a
                      key={d}
                      href={'https://' + d}
                      target="_blank"
                      rel="noreferrer"
                      className="font-mono text-[13px] hover:underline"
                    >
                      {d}
                    </a>
                  ))}
                </div>
              </Fact>
            )}
            {project.private ? (
              <Fact label="Public address">
                <span className="text-sm">None, this service is private</span>
              </Fact>
            ) : (
              <Fact label="Skiff address">
                <a
                  href={project.url}
                  target="_blank"
                  rel="noreferrer"
                  className="font-mono text-[13px] hover:underline"
                >
                  {skiffHost}
                </a>
              </Fact>
            )}
            <Fact label="Internal address">
              <span
                className="font-mono text-[13px]"
                title="How your team's other services reach this one. Needs a deploy after upgrading."
              >
                {project.internalUrl}
              </span>
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
          {project.setupNote && <SetupNote app={project.name} note={project.setupNote} />}
          {latest?.status === 'failed' && (
            <p className="text-danger pb-5 text-sm">
              {live && project.state === 'running'
                ? 'The last deploy failed, so the previous version is still serving. The log below says why.'
                : 'The last deploy failed and nothing is serving. The log below says why.'}
            </p>
          )}
          <section className="dark bg-well text-foreground border-seam -mx-4 min-h-[40vh] flex-1 border-t-2 px-4 py-5 sm:-mx-8 sm:px-8">
            <h2 className="text-muted-foreground mb-3 font-mono text-[11px] tracking-wider uppercase">
              {latest ? 'Latest deploy' : 'Build log'}
            </h2>
            {cli ? (
              <p className="text-foreground/60 text-sm">
                This app builds on the machine that runs{' '}
                <span className="font-mono">skiff deploy</span>, so its log stays there.
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

        <TabsContent value="deployments" className="pb-[46vh]">
          <DeploymentsPanel
            project={project.name}
            canRedeploy={!cli}
            onRollback={term.rollback}
            onRedeploy={term.redeploy}
          />
        </TabsContent>

        {!cli && !fromImage && !project.parent && (
          <TabsContent value="previews" className="pb-[46vh]">
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
          </TabsContent>
        )}

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
            <section className="border-foreground/8 mt-10 border-t pt-6">
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
            isPrivate={project.private}
            rateLimit={project.rateLimit}
            memory={project.memory}
            cpu={project.cpu}
            onSaved={reload}
            cli={cli}
          />
          {fromImage && <RegistryPanel app={project.name} />}
          <VolumesPanel app={project.name} onRedeploy={() => term.redeploy(project.name)} />
          <TCPPortSection app={project.name} />
          <DangerZone name={project.name} onDeleted={() => navigate('/')} />
        </TabsContent>
      </Tabs>

      {consoleOpen && <ConsoleDrawer app={project.name} onClose={() => setConsoleOpen(false)} />}
      {term.stream && <Drawer stream={term.stream} onClose={term.close} onStop={term.stop} />}
    </div>
  )
}

// Stays until dismissed: on most catalog apps, the first visitor becomes the admin.
function SetupNote({ app, note }: { app: string; note: string }) {
  const key = 'skiff-setup-done-' + app
  const [hidden, setHidden] = useState(() => {
    try {
      return localStorage.getItem(key) === '1'
    } catch {
      return false
    }
  })
  if (hidden) return null
  return (
    <div className="border-warning/30 bg-warning/5 mb-5 flex items-start justify-between gap-4 rounded-lg border p-3 text-xs leading-relaxed">
      <p>
        <strong className="font-semibold">Do this first.</strong> {note}
      </p>
      <button
        onClick={() => {
          try {
            localStorage.setItem(key, '1')
          } catch {
            /* it comes back next visit */
          }
          setHidden(true)
        }}
        className="text-muted-foreground hover:text-foreground shrink-0 underline underline-offset-2"
      >
        Done
      </button>
    </div>
  )
}

const capitalize = (s: string) => s.charAt(0).toUpperCase() + s.slice(1)
