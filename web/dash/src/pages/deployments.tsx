import { useQueryClient } from '@tanstack/react-query'
import { GitCommitHorizontal, Search, Square } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router'

import { Button } from '@/components/ui/button'
import { Drawer } from '@/components/drawer'
import { ErrorState } from '@/components/error-state'
import { FeedSkeleton } from '@/components/skeletons'
import { queryKeys } from '@/constants/query-keys'
import { useAllDeploys } from '@/hooks/use-all-deploys'
import { useConsole } from '@/hooks/use-console'
import { relTime } from '@/lib/format'
import { deploysService, type Deploy } from '@/services/api.service'
import { deploymentPath } from './project/deployments-panel'
import { statusLabel } from './project/status'

const CONTROL_PLANE = 'panel'

const deployDot = (status: string): string =>
  status === 'live'
    ? 'bg-success'
    : status === 'failed'
      ? 'bg-danger'
      : status === 'canceled'
        ? 'bg-foreground/25'
        : 'bg-warning pulse-dot'

type Filter = 'all' | 'live' | 'building' | 'failed'

const FILTERS: { key: Filter; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'live', label: 'Ready' },
  { key: 'building', label: 'Building' },
  { key: 'failed', label: 'Failed' },
]

const matchesFilter = (d: Deploy, f: Filter): boolean => (f === 'all' ? true : d.status === f)

export default function DeploymentsPage() {
  const { data, isPending, isError, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useAllDeploys()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const term = useConsole(() => {})
  const [q, setQ] = useState('')
  const [filter, setFilter] = useState<Filter>('all')

  const deploys = useMemo(() => {
    const seen = new Set<string>()
    const out: Deploy[] = []
    for (const d of data?.pages.flat() ?? []) {
      const key = d.app + d.id
      if (!seen.has(key)) {
        seen.add(key)
        out.push(d)
      }
    }
    return out
  }, [data])

  const cancel = async (app: string, id: string) => {
    await deploysService.cancel(app, id)
    qc.invalidateQueries({ queryKey: queryKeys.deploysAll })
  }

  const rows = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return deploys.filter((d) => {
      if (!matchesFilter(d, filter)) return false
      if (!needle) return true
      return (
        d.app.toLowerCase().includes(needle) ||
        d.message.toLowerCase().includes(needle) ||
        d.commit.toLowerCase().includes(needle) ||
        d.trigger.toLowerCase().includes(needle)
      )
    })
  }, [deploys, q, filter])

  return (
    <div className="px-8 py-8">
      <header className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight">Deployments</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Every build across your projects, newest first.
        </p>
      </header>

      {/* toolbar */}
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div className="relative sm:max-w-xs sm:flex-1">
          <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-3 h-3.5 w-3.5 -translate-y-1/2" />
          <input
            value={q}
            onChange={(e) => setQ(e.target.value)}
            placeholder="Search project, commit, message…"
            className="border-foreground/12 bg-foreground/2 placeholder:text-foreground/30 focus-visible:border-foreground/25 h-9 w-full rounded-[6px] border pr-3 pl-8.5 text-sm outline-none"
          />
        </div>
        <div className="border-foreground/10 bg-foreground/2 flex shrink-0 items-center gap-1 rounded-[6px] border p-0.5">
          {FILTERS.map((f) => (
            <button
              key={f.key}
              onClick={() => setFilter(f.key)}
              className={
                'rounded-[4px] px-2.5 py-1 text-xs font-medium transition-colors ' +
                (filter === f.key
                  ? 'text-foreground bg-foreground/10'
                  : 'text-muted-foreground hover:text-foreground')
              }
            >
              {f.label}
            </button>
          ))}
        </div>
      </div>

      {/* feed */}
      {isPending ? (
        <FeedSkeleton rows={7} />
      ) : isError && deploys.length === 0 ? (
        <ErrorState message="Couldn't load deployments — retrying…" />
      ) : (
        <>
          <div className="border-foreground/8 overflow-hidden rounded-xl border">
            {rows.length === 0 ? (
              <p className="text-muted-foreground p-10 text-center text-sm">
                {deploys.length === 0
                  ? 'No deployments yet — deploy a project to see its build history here.'
                  : 'No deployments match your filters.'}
              </p>
            ) : (
              rows.map((d) => (
                <div
                  key={d.app + d.id}
                  className="group border-foreground/5 hover:bg-foreground/3 flex items-center border-b pr-3 transition-colors last:border-0"
                >
                  <button
                    onClick={() =>
                      d.app === CONTROL_PLANE
                        ? term.showBuildLog(d.app, d.id)
                        : navigate(deploymentPath(d))
                    }
                    className="flex min-w-0 flex-1 items-center gap-3.5 px-5 py-3.5 text-left"
                  >
                    <span className={'h-2 w-2 shrink-0 rounded-full ' + deployDot(d.status)} />

                    {/* project avatar */}
                    <span className="border-foreground/10 from-foreground/12 to-foreground/2 text-foreground/80 grid h-7 w-7 shrink-0 place-items-center rounded-md border bg-linear-to-br font-mono text-xs font-semibold">
                      {d.app.charAt(0).toUpperCase()}
                    </span>

                    <div className="min-w-0 flex-1">
                      <div className="flex items-center gap-2">
                        <Link
                          to={'/projects/' + d.app}
                          onClick={(e) => e.stopPropagation()}
                          className="hover:text-foreground truncate text-sm font-medium hover:underline"
                        >
                          {d.app}
                        </Link>
                        <span className="text-muted-foreground shrink-0 text-[11px]">
                          {statusLabel(d.status)}
                        </span>
                      </div>
                      <p className="text-muted-foreground mt-0.5 truncate text-xs">
                        {d.message || 'No commit message'}
                      </p>
                    </div>

                    {d.commit && (
                      <span className="text-muted-foreground hidden shrink-0 items-center gap-1 font-mono text-xs sm:flex">
                        <GitCommitHorizontal className="h-3.5 w-3.5 opacity-60" />
                        {d.commit}
                      </span>
                    )}
                    <span className="text-muted-foreground hidden w-14 shrink-0 truncate font-mono text-[11px] md:block">
                      {d.trigger}
                    </span>
                    <span className="text-muted-foreground w-16 shrink-0 text-right font-mono text-[11px]">
                      {relTime(d.started)}
                    </span>
                  </button>

                  {d.status === 'building' && (
                    <button
                      onClick={() => cancel(d.app, d.id)}
                      title="Stop this build"
                      className="border-foreground/10 text-danger hover:border-danger/30 hover:bg-danger/10 ml-1 flex shrink-0 items-center gap-1 rounded-[6px] border px-2 py-1 text-[11px] transition"
                    >
                      <Square className="h-3 w-3 fill-current" />
                      Stop
                    </button>
                  )}
                </div>
              ))
            )}
          </div>
          {hasNextPage && (
            <div className="mt-4 flex justify-center">
              <Button
                variant="outline"
                size="sm"
                loading={isFetchingNextPage}
                onClick={() => fetchNextPage()}
              >
                Load more
              </Button>
            </div>
          )}
        </>
      )}

      {term.stream && <Drawer stream={term.stream} onClose={term.close} onStop={term.stop} />}
    </div>
  )
}
