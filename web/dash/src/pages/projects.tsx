import { Plus, Search } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'

import { AppCard } from '@/components/app-card'
import { ControlPlaneCard } from '@/components/control-plane-card'
import { DeployHistory } from '@/components/deploy-history'
import { DeployModal } from '@/components/deploy-modal'
import { Drawer } from '@/components/drawer'
import { EnvDialog } from '@/components/env-dialog'
import { ErrorState } from '@/components/error-state'
import { LogoMark } from '@/components/logo'
import { OnboardingChecklist } from '@/components/onboarding-checklist'
import { CardGridSkeleton } from '@/components/skeletons'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { useAuthContext } from '@/context/auth-context'
import { useApps } from '@/hooks/use-apps'
import { useConsole } from '@/hooks/use-console'
import { useDeploys } from '@/hooks/use-deploys'
import { useSystem } from '@/hooks/use-system'

export default function ProjectsPage() {
  const { me } = useAuthContext()
  const { apps, isPending, isError, reload, stop } = useApps()
  const term = useConsole(reload)
  const history = useDeploys()
  const { info: system } = useSystem()
  const [open, setOpen] = useState(false)
  const [envApp, setEnvApp] = useState<string | null>(null)
  const [q, setQ] = useState('')

  const teamName = me?.teams?.find((t) => t.id === me?.team)?.name ?? 'this team'

  useEffect(() => {
    reload()
  }, [me?.team, reload])

  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase()
    if (!needle) return apps
    return apps.filter(
      (a) => a.name.toLowerCase().includes(needle) || (a.repo ?? '').toLowerCase().includes(needle),
    )
  }, [apps, q])

  const showControlPlane = !q.trim() && !!system?.selfDeploy
  const cpOffset = showControlPlane ? 1 : 0

  return (
    <div className="px-8 py-8">
      <header className="mb-7 flex flex-wrap items-end justify-between gap-4">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Projects</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            {isPending ? '…' : `${apps.length} ${apps.length === 1 ? 'app' : 'apps'}`} in{' '}
            <span className="text-foreground/70">{teamName}</span>.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <div className="relative">
            <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2" />
            <Input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder="Search projects…"
              className="h-8 w-52 pl-8 font-mono text-xs"
            />
          </div>
          <Button size="sm" onClick={() => setOpen(true)}>
            <Plus />
            New project
          </Button>
        </div>
      </header>

      <OnboardingChecklist appsCount={apps.length} onDeploy={() => setOpen(true)} />

      <div className="grid grid-cols-[repeat(auto-fill,minmax(320px,1fr))] gap-4 pb-[46vh]">
        {isPending ? (
          <CardGridSkeleton count={6} />
        ) : isError && apps.length === 0 ? (
          <div className="col-span-full">
            <ErrorState message="Couldn't load your projects — retrying…" />
          </div>
        ) : (
          <>
            {showControlPlane && (
              <ControlPlaneCard
                info={system!}
                onHistory={history.open}
                onLogs={(app, id) => {
                  history.close()
                  term.showBuildLog(app, id)
                }}
              />
            )}

            {filtered.map((a, i) => (
              <AppCard
                key={a.name}
                app={a}
                index={i + cpOffset}
                onLogs={term.showLogs}
                onHistory={history.open}
                onEnv={setEnvApp}
                onStop={stop}
              />
            ))}

            {apps.length === 0 && !q.trim() && (
              <div className="col-span-full flex flex-col items-center gap-4 rounded-xl border border-dashed border-white/10 py-16 text-center">
                <LogoMark className="h-9 w-9 opacity-30" />
                <div className="space-y-1">
                  <p className="text-sm font-medium">No projects yet</p>
                  <p className="text-muted-foreground text-sm">
                    Deploy your first app from a Git repository or a Docker image.
                  </p>
                </div>
                <Button size="sm" onClick={() => setOpen(true)}>
                  <Plus />
                  New project
                </Button>
              </div>
            )}

            {apps.length > 0 && filtered.length === 0 && (
              <p className="text-muted-foreground col-span-full py-12 text-center text-sm">
                No projects match “{q}”.
              </p>
            )}
          </>
        )}
      </div>

      <DeployModal
        open={open}
        onOpenChange={setOpen}
        onDeployUrl={(git, name, port, token) => {
          setOpen(false)
          term.deploy(git, name, port, token)
        }}
        onDeployRepo={(repo, clone, branch, name, port, auto, rootDir) => {
          setOpen(false)
          term.deployRepo(repo, clone, branch, name, port, auto, rootDir)
        }}
        onDeployImage={(app, id) => {
          setOpen(false)
          term.follow(app, id)
        }}
      />

      <DeployHistory
        app={history.app}
        deploys={history.deploys}
        onClose={history.close}
        onViewLog={(app, id) => {
          history.close()
          term.showBuildLog(app, id)
        }}
      />

      <EnvDialog app={envApp} onClose={() => setEnvApp(null)} />

      {term.stream && <Drawer stream={term.stream} onClose={term.close} onStop={term.stop} />}
    </div>
  )
}
