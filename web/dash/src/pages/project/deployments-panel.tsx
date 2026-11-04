import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router'
import { useQueryClient } from '@tanstack/react-query'
import {
  GitBranch,
  MoreHorizontal,
  RotateCcw,
  RotateCw,
  ScrollText,
  Search,
  Square,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { ErrorState } from '@/components/error-state'
import { FeedSkeleton } from '@/components/skeletons'
import { useDeployBranches, useProjectDeploys } from '@/hooks/use-project-deploys'
import { errText } from '@/lib/errors'
import { relTime } from '@/lib/format'
import { useConfirm } from '@/providers/confirm-provider'
import { deploysService, type Deploy, type DeployFilters } from '@/services/api.service'
import { DeployDuration } from './deploy-duration'
import { deployDot, shortDeployId, statusLabel, triggerLabel } from './status'

const ALL = 'all'

export const deploymentPath = (d: Pick<Deploy, 'app' | 'id'>) =>
  `/projects/${d.app}/deployments/${d.id}`

export function EnvironmentLabel({ deploy }: { deploy: Pick<Deploy, 'parent' | 'current'> }) {
  return (
    <span className="flex flex-wrap items-center gap-1.5">
      <span className="text-muted-foreground text-xs">
        {deploy.parent ? 'Preview' : 'Production'}
      </span>
      {deploy.current && (
        <span className="border-success/25 bg-success/10 text-success rounded-full border px-1.5 py-px font-mono text-[9px] font-medium tracking-wider uppercase">
          Current
        </span>
      )}
    </span>
  )
}

export function DeploymentsPanel({
  project,
  canRedeploy,
  onRollback,
  onRedeploy,
}: {
  project: string
  canRedeploy: boolean
  onRollback: (app: string, id: string) => void
  onRedeploy: (app: string) => void
}) {
  const [env, setEnv] = useState(ALL)
  const [status, setStatus] = useState(ALL)
  const [branch, setBranch] = useState(ALL)
  const [input, setInput] = useState('')
  const [q, setQ] = useState('')
  useEffect(() => {
    const t = setTimeout(() => setQ(input.trim()), 300)
    return () => clearTimeout(t)
  }, [input])

  const filters = useMemo<DeployFilters>(
    () => ({
      ...(env !== ALL && { env: env as DeployFilters['env'] }),
      ...(status !== ALL && { status }),
      ...(branch !== ALL && { branch }),
      ...(q && { q }),
    }),
    [env, status, branch, q],
  )
  const filtered = Object.keys(filters).length > 0
  const pages = useProjectDeploys(project, filters)
  const { data: branches = [] } = useDeployBranches(project)
  const deploys = useMemo(() => {
    const seen = new Set<string>()
    return (pages.data?.pages.flat() ?? []).filter((d) => {
      const key = d.app + d.id
      if (seen.has(key)) return false
      seen.add(key)
      return true
    })
  }, [pages.data])

  const clear = () => {
    setEnv(ALL)
    setStatus(ALL)
    setBranch(ALL)
    setInput('')
    setQ('')
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Select value={env} onValueChange={setEnv}>
          <SelectTrigger size="sm" className="w-44" aria-label="Environment">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>All environments</SelectItem>
            <SelectItem value="production">Production</SelectItem>
            <SelectItem value="preview">Preview</SelectItem>
          </SelectContent>
        </Select>
        <Select value={status} onValueChange={setStatus}>
          <SelectTrigger size="sm" className="w-36" aria-label="Status">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={ALL}>All statuses</SelectItem>
            <SelectItem value="live">Ready</SelectItem>
            <SelectItem value="building">Building</SelectItem>
            <SelectItem value="failed">Failed</SelectItem>
            <SelectItem value="canceled">Canceled</SelectItem>
          </SelectContent>
        </Select>
        {branches.length > 0 && (
          <Select value={branch} onValueChange={setBranch}>
            <SelectTrigger size="sm" className="w-40" aria-label="Branch">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value={ALL}>All branches</SelectItem>
              {branches.map((b) => (
                <SelectItem key={b} value={b}>
                  {b}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        <div className="relative min-w-48 flex-1 sm:ml-auto sm:max-w-64">
          <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2.5 h-3.5 w-3.5 -translate-y-1/2" />
          <input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="Search commits…"
            aria-label="Search commits"
            className="border-foreground/12 bg-inset placeholder:text-foreground/30 focus-visible:border-foreground/30 h-8 w-full rounded-[6px] border pr-3 pl-8 text-sm outline-none"
          />
        </div>
      </div>

      {pages.isPending ? (
        <FeedSkeleton rows={4} />
      ) : pages.isError ? (
        <ErrorState message="Couldn't load this project's deployments." />
      ) : deploys.length === 0 ? (
        <div className="text-muted-foreground border-foreground/8 flex flex-col items-center gap-3 rounded-xl border py-12 text-sm">
          {filtered ? 'No deployments match these filters.' : 'No deployments yet.'}
          {filtered && (
            <Button size="sm" variant="outline" onClick={clear}>
              Clear filters
            </Button>
          )}
        </div>
      ) : (
        <div className="border-foreground/8 overflow-hidden rounded-xl border">
          {deploys.map((d) => (
            <DeploymentRow
              key={d.app + d.id}
              deploy={d}
              canRedeploy={canRedeploy}
              onRollback={onRollback}
              onRedeploy={onRedeploy}
            />
          ))}
        </div>
      )}

      {pages.hasNextPage && (
        <div className="flex justify-center pt-1">
          <Button
            variant="outline"
            size="sm"
            loading={pages.isFetchingNextPage}
            onClick={() => pages.fetchNextPage()}
          >
            Load more
          </Button>
        </div>
      )}
    </div>
  )
}

function DeploymentRow({
  deploy: d,
  canRedeploy,
  onRollback,
  onRedeploy,
}: {
  deploy: Deploy
  canRedeploy: boolean
  onRollback: (app: string, id: string) => void
  onRedeploy: (app: string) => void
}) {
  const navigate = useNavigate()
  const qc = useQueryClient()
  const confirm = useConfirm()
  const [error, setError] = useState('')
  const path = deploymentPath(d)

  const stop = async () => {
    setError('')
    try {
      await deploysService.cancel(d.app, d.id)
      void qc.invalidateQueries({ queryKey: ['deploys', d.parent ?? d.app] })
    } catch (e) {
      setError(errText(e, "Couldn't stop this build. Try again."))
    }
  }

  return (
    <div className="border-foreground/5 hover:bg-foreground/3 border-b transition-colors last:border-0">
      <div className="flex items-start gap-1 pr-2">
        <Link
          to={path}
          className="focus-visible:bg-foreground/5 grid min-w-0 flex-1 grid-cols-2 gap-x-5 gap-y-2 py-3 pl-4 outline-none md:grid-cols-[minmax(0,1fr)_7rem_minmax(0,1.5fr)_6.5rem]"
        >
          <div className="min-w-0">
            <p className="font-mono text-sm font-medium">{shortDeployId(d.id)}</p>
            <div className="mt-0.5">
              <EnvironmentLabel deploy={d} />
            </div>
          </div>

          <div className="order-3 md:order-none">
            <p className="flex items-center gap-1.5 text-sm">
              <span className={'h-2 w-2 shrink-0 rounded-full ' + deployDot(d.status)} />
              {statusLabel(d.status)}
            </p>
            <p className="text-muted-foreground mt-0.5 pl-3.5 font-mono text-[11px]">
              <DeployDuration deploy={d} />
            </p>
          </div>

          <div className="order-4 col-span-2 min-w-0 md:order-none md:col-span-1">
            <p className="text-foreground/80 flex items-center gap-1 truncate font-mono text-xs">
              {d.branch && (
                <>
                  <GitBranch className="h-3 w-3 shrink-0 opacity-60" />
                  <span className="truncate">{d.branch}</span>
                </>
              )}
              {d.commit && (
                <span className="text-muted-foreground shrink-0">
                  {d.branch && '· '}
                  {d.commit}
                </span>
              )}
            </p>
            <p className="text-muted-foreground mt-0.5 truncate text-xs">
              {d.message || 'No commit message'}
            </p>
          </div>

          <div className="order-2 text-right md:order-none">
            <p className="text-sm">{relTime(d.started)}</p>
            <p className="text-muted-foreground mt-0.5 truncate text-xs">
              {d.actor || triggerLabel(d.trigger)}
            </p>
          </div>
        </Link>

        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              size="icon-sm"
              variant="ghost"
              className="text-muted-foreground mt-2.5 shrink-0"
              aria-label={'Actions for deployment ' + shortDeployId(d.id)}
            >
              <MoreHorizontal />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-48">
            <DropdownMenuItem className="gap-2" onClick={() => navigate(path)}>
              <ScrollText className="h-3.5 w-3.5" />
              Build log
            </DropdownMenuItem>
            {d.status === 'live' && (
              <DropdownMenuItem className="gap-2" onClick={() => navigate(path + '?tab=runtime')}>
                <ScrollText className="h-3.5 w-3.5" />
                Runtime logs
              </DropdownMenuItem>
            )}
            {d.current && canRedeploy && (
              <DropdownMenuItem className="gap-2" onClick={() => onRedeploy(d.app)}>
                <RotateCw className="h-3.5 w-3.5" />
                Redeploy
              </DropdownMenuItem>
            )}
            {d.rollbackable && (
              <DropdownMenuItem
                className="gap-2"
                onClick={async () => {
                  if (
                    await confirm({
                      title: `Roll back ${d.app}?`,
                      description: 'It re-runs this build straight away, with no rebuild.',
                      confirmText: 'Roll back',
                    })
                  )
                    onRollback(d.app, d.id)
                }}
              >
                <RotateCcw className="h-3.5 w-3.5" />
                Roll back to this
              </DropdownMenuItem>
            )}
            {d.status === 'building' && (
              <DropdownMenuItem className="text-danger gap-2" onClick={stop}>
                <Square className="h-3.5 w-3.5 fill-current" />
                Stop build
              </DropdownMenuItem>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
      </div>
      {error && <p className="text-danger px-4 pb-3 text-xs">{error}</p>}
    </div>
  )
}
