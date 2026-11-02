import { Link } from 'react-router'
import { AlertTriangle, ChevronRight, HardDrive, Loader2, MemoryStick, Power } from 'lucide-react'
import { relTime } from '@/lib/format'
import type { AttentionItem, Overview } from '@/services/api.service'

function Row({ item }: { item: AttentionItem }) {
  const copy = describe(item)
  return (
    <Link
      to={copy.to}
      className="flex items-center gap-3 px-4 py-3 transition-colors hover:bg-foreground/4"
    >
      <copy.icon className={'h-4 w-4 shrink-0 ' + copy.tone} />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{copy.title}</p>
        <p className="text-muted-foreground truncate text-xs">{copy.detail}</p>
      </div>
      <span className="text-muted-foreground flex shrink-0 items-center gap-1 text-xs">
        {copy.action}
        <ChevronRight className="h-3.5 w-3.5" />
      </span>
    </Link>
  )
}

function describe(item: AttentionItem) {
  const project = '/projects/' + item.app
  switch (item.kind) {
    case 'deploy.failed':
      return {
        icon: AlertTriangle,
        tone: 'text-danger',
        to: project,
        title: `${item.app}: the last deploy failed`,
        detail:
          (item.serving ? 'The previous version is still serving. ' : "It isn't serving anything. ") +
          (item.at ? `Failed ${relTime(item.at)}.` : ''),
        action: 'See why',
      }
    case 'app.down':
      return {
        icon: Power,
        tone: 'text-danger',
        to: project,
        title: `${item.app} isn't running`,
        detail: `Its container is ${item.state}.`,
        action: 'Open',
      }
    case 'oom':
      return {
        icon: MemoryStick,
        tone: 'text-danger',
        to: project,
        title: `${item.app} ran out of memory`,
        detail: `It hit its memory limit and was restarted${item.at ? ` ${relTime(item.at)}` : ''}. It may need a higher limit, or have a leak.`,
        action: 'Open',
      }
    case 'disk':
      return {
        icon: HardDrive,
        tone: 'text-warning',
        to: '/server',
        title: `Your server's disk is ${item.percent}% full`,
        detail: 'Deploys start failing when it runs out. Old images and build cache can be cleared.',
        action: 'Free up space',
      }
    default:
      return {
        icon: MemoryStick,
        tone: 'text-warning',
        to: '/server',
        title: `Your server's memory is ${item.percent}% used`,
        detail: 'Apps may be stopped by the system if it runs out.',
        action: 'See what’s using it',
      }
  }
}

function headline(o: Overview) {
  const n = o.attention.length
  if (n > 0) return n === 1 ? 'One thing needs attention' : `${n} things need attention`
  if (o.projects === 0) return 'Nothing deployed yet'
  if (o.running + o.sleeping === 0) return 'Nothing is running yet'
  return o.projects === 1 ? 'Your project is running' : `All ${o.projects} projects are running`
}

export function FleetHeadline({ overview, teamName }: { overview?: Overview; teamName: string }) {
  const count = overview?.projects ?? 0
  const sleeping = overview?.sleeping ?? 0
  return (
    <div>
      <h1 className="text-2xl font-bold tracking-tight">
        {overview ? headline(overview) : 'Projects'}
      </h1>
      <p className="text-muted-foreground mt-1 text-sm">
        {count > 0 &&
          `${count} ${count === 1 ? 'project' : 'projects'}${sleeping > 0 ? `, ${sleeping} asleep,` : ''} `}
        in <span className="text-foreground/70">{teamName}</span>
      </p>
    </div>
  )
}

export function AttentionList({ overview }: { overview?: Overview }) {
  if (!overview || (overview.attention.length === 0 && overview.building.length === 0)) return null
  return (
    <div className="divide-y divide-foreground/8 overflow-hidden rounded-xl border border-foreground/10 bg-card">
      {overview.attention.map((item) => (
        <Row key={item.kind + (item.app ?? '')} item={item} />
      ))}
      {overview.building.map((app) => (
        <Link
          key={app}
          to={'/projects/' + app}
          className="flex items-center gap-3 px-4 py-3 text-sm transition-colors hover:bg-foreground/4"
        >
          <Loader2 className="text-muted-foreground h-4 w-4 shrink-0 animate-spin" />
          <span className="min-w-0 flex-1 truncate">{app} is deploying</span>
          <ChevronRight className="text-muted-foreground h-3.5 w-3.5 shrink-0" />
        </Link>
      ))}
    </div>
  )
}
