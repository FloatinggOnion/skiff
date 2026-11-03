import { relTime } from '@/lib/format'
import { useMemo, type ComponentType } from 'react'
import { useInfiniteQuery } from '@tanstack/react-query'
import {
  Rocket,
  RotateCcw,
  Settings,
  KeyRound,
  GitBranch,
  Terminal,
  Trash2,
  Database,
  Undo2,
  Globe,
  Mail,
  UserPlus,
  UserMinus,
  Bell,
  History,
  GitCommitHorizontal,
} from 'lucide-react'
import { auditService, type AuditEntry } from '@/services/api.service'
import { queryKeys } from '@/constants/query-keys'
import { FeedSkeleton } from '@/components/skeletons'
import { ErrorState } from '@/components/error-state'
import { Button } from '@/components/ui/button'

const ACTIVITY_PAGE = 30

type Kind = { icon: ComponentType<{ className?: string }>; verb: string; tone: string }

const KINDS: Record<string, Kind> = {
  deploy: { icon: Rocket, verb: 'deployed', tone: 'text-success' },
  rollback: { icon: RotateCcw, verb: 'rolled back', tone: 'text-warning' },
  'settings.update': {
    icon: Settings,
    verb: 'updated settings for',
    tone: 'text-muted-foreground',
  },
  'env.update': { icon: KeyRound, verb: 'changed environment for', tone: 'text-chart-violet' },
  'project.delete': { icon: Trash2, verb: 'deleted project', tone: 'text-danger' },
  'database.create': { icon: Database, verb: 'created database', tone: 'text-success' },
  'database.delete': { icon: Database, verb: 'deleted database', tone: 'text-danger' },
  'database.shell': { icon: Terminal, verb: 'opened a shell in database', tone: 'text-warning' },
  'database.public': { icon: Database, verb: 'made public database', tone: 'text-warning' },
  'database.private': { icon: Database, verb: 'made private database', tone: 'text-muted-foreground' },
  'stack.preview': { icon: GitBranch, verb: 'started a preview,', tone: 'text-success' },
  'stack.preview-data': { icon: Settings, verb: 'changed previews for', tone: 'text-muted-foreground' },
  'console.open': { icon: Terminal, verb: 'opened a console in', tone: 'text-warning' },
  'backup.restore': { icon: Undo2, verb: 'restored a backup of', tone: 'text-warning' },
  'domain.add': { icon: Globe, verb: 'added domain', tone: 'text-success' },
  'domain.remove': { icon: Globe, verb: 'removed domain', tone: 'text-danger' },
  'mail.add': { icon: Mail, verb: 'set up forwarding for', tone: 'text-success' },
  'mail.remove': { icon: Mail, verb: 'stopped forwarding', tone: 'text-danger' },
  'member.invite': { icon: UserPlus, verb: 'invited', tone: 'text-success' },
  'member.remove': { icon: UserMinus, verb: 'removed', tone: 'text-danger' },
  'alerts.update': { icon: Bell, verb: 'updated alert channels', tone: 'text-muted-foreground' },
  'token.create': { icon: KeyRound, verb: 'created API token', tone: 'text-success' },
  'token.revoke': { icon: KeyRound, verb: 'revoked an API token', tone: 'text-danger' },
}

export default function ActivityPage() {
  const { data, isPending, isError, fetchNextPage, hasNextPage, isFetchingNextPage } =
    useInfiniteQuery({
      queryKey: queryKeys.audit,
      queryFn: ({ pageParam }) => auditService.list(pageParam, ACTIVITY_PAGE),
      initialPageParam: undefined as number | undefined,
      getNextPageParam: (last) =>
        last.length === ACTIVITY_PAGE ? last[last.length - 1].id : undefined,
      refetchInterval: 15000,
    })

  const entries = useMemo(() => {
    const seen = new Set<number>()
    const out: AuditEntry[] = []
    for (const e of data?.pages.flat() ?? []) {
      if (!seen.has(e.id)) {
        seen.add(e.id)
        out.push(e)
      }
    }
    return out
  }, [data])

  return (
    <div className="px-8 py-8">
      <header className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight">Activity</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Every deploy, change, and teardown in this team — who did what, and when.
        </p>
      </header>

      {isPending ? (
        <FeedSkeleton rows={6} />
      ) : isError && entries.length === 0 ? (
        <ErrorState message="Couldn't load activity — retrying…" />
      ) : entries.length === 0 ? (
        <div className="text-muted-foreground flex flex-col items-center gap-2 rounded-xl border border-foreground/8 py-20 text-sm">
          <History className="h-6 w-6 opacity-40" />
          <span>No activity yet — actions across the team will show up here.</span>
        </div>
      ) : (
        <>
          <ol className="overflow-hidden rounded-xl border border-foreground/8">
            {entries.map((e, i) => (
              <Row key={e.id} e={e} last={i === entries.length - 1} />
            ))}
          </ol>
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
    </div>
  )
}

function Row({ e, last }: { e: AuditEntry; last: boolean }) {
  const k = KINDS[e.action] ?? {
    icon: GitCommitHorizontal,
    verb: e.action,
    tone: 'text-muted-foreground',
  }
  const Icon = k.icon
  return (
    <li className={'flex items-start gap-3 px-4 py-3 ' + (last ? '' : 'border-b border-foreground/6')}>
      <span
        className={
          'mt-0.5 grid h-7 w-7 shrink-0 place-items-center rounded-full bg-foreground/5 ' + k.tone
        }
      >
        <Icon className="h-3.5 w-3.5" />
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-sm">
          <Actor actor={e.actor} /> <span className="text-muted-foreground">{k.verb}</span>{' '}
          {e.target && <span className="font-mono text-[13px]">{e.target}</span>}
        </p>
        {e.detail && <p className="text-muted-foreground mt-0.5 text-xs">{e.detail}</p>}
      </div>
      <time className="text-muted-foreground shrink-0 text-xs tabular-nums">
        {relTime(e.created)}
      </time>
    </li>
  )
}

function Actor({ actor }: { actor: string }) {
  // System actors ("push", "token:<name>") render as a chip; users as a name.
  if (actor === 'push' || actor.startsWith('token:')) {
    return (
      <span className="text-muted-foreground rounded bg-foreground/8 px-1.5 py-0.5 font-mono text-[11px]">
        {actor.startsWith('token:') ? actor.slice('token:'.length) + ' (token)' : actor}
      </span>
    )
  }
  return <span className="font-medium">{actor.includes('@') ? actor.split('@')[0] : actor}</span>
}
