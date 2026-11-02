import { useState } from 'react'
import { useNavigate } from 'react-router'
import { Bell } from 'lucide-react'
import { useAllDeploys } from '@/hooks/use-all-deploys'
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
} from '@/components/ui/dropdown-menu'

const SEEN_KEY = 'skiff-notifs-seen'

function rel(unix: number): string {
  const s = Math.max(0, Math.floor(Date.now() / 1000 - unix))
  if (s < 60) return s + 's'
  if (s < 3600) return Math.floor(s / 60) + 'm'
  if (s < 86400) return Math.floor(s / 3600) + 'h'
  return Math.floor(s / 86400) + 'd'
}

const dot = (status: string): string =>
  status === 'live'
    ? 'bg-success'
    : status === 'failed'
      ? 'bg-danger'
      : status === 'canceled'
        ? 'bg-foreground/25'
        : 'bg-warning pulse-dot'

type Filter = 'all' | 'live' | 'failed'
const FILTERS: { key: Filter; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'live', label: 'Deployed' },
  { key: 'failed', label: 'Failed' },
]

export default function NotificationBell() {
  const { data } = useAllDeploys()
  const deploys = data?.pages[0] ?? [] // newest page is plenty for the bell
  const navigate = useNavigate()
  const [seen, setSeen] = useState<number>(() => Number(localStorage.getItem(SEEN_KEY) || 0))
  const [filter, setFilter] = useState<Filter>('all')

  const unread = deploys.slice(0, 8).filter((d) => d.started > seen).length
  const recent = deploys.filter((d) => filter === 'all' || d.status === filter).slice(0, 12)

  const markSeen = () => {
    const latest = recent[0]?.started ?? Math.floor(Date.now() / 1000)
    localStorage.setItem(SEEN_KEY, String(latest))
    setSeen(latest)
  }

  return (
    <DropdownMenu
      onOpenChange={(o) => {
        if (o) markSeen()
      }}
    >
      <DropdownMenuTrigger asChild>
        <button
          aria-label="Activity"
          className="text-muted-foreground hover:text-foreground relative grid h-8 w-8 place-items-center rounded-md transition-colors hover:bg-foreground/5"
        >
          <Bell className="h-4 w-4" />
          {unread > 0 && (
            <span className="ring-background absolute top-1.5 right-1.5 h-2 w-2 rounded-full bg-success ring-2" />
          )}
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-80 p-0">
        <div className="flex items-center justify-between gap-2 border-b border-foreground/8 px-3 py-2">
          <span className="text-xs font-medium">Activity</span>
          <div className="flex items-center gap-0.5 rounded-[6px] border border-foreground/10 p-0.5">
            {FILTERS.map((f) => (
              <button
                key={f.key}
                onClick={(e) => {
                  e.preventDefault()
                  setFilter(f.key)
                }}
                className={
                  'rounded-[4px] px-1.5 py-0.5 text-[10px] font-medium transition-colors ' +
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
        {recent.length === 0 ? (
          <p className="text-muted-foreground p-6 text-center text-xs">
            {filter === 'all' ? 'No deployments yet.' : 'Nothing matches this filter.'}
          </p>
        ) : (
          <div className="max-h-96 overflow-auto py-1">
            {recent.map((d) => (
              <button
                key={d.app + d.id}
                onClick={() => navigate('/projects/' + d.app)}
                className="flex w-full items-center gap-2.5 px-3 py-2 text-left transition-colors hover:bg-foreground/5"
              >
                <span className={'h-1.5 w-1.5 shrink-0 rounded-full ' + dot(d.status)} />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-xs font-medium">
                    <span className="font-mono">{d.app}</span>{' '}
                    <span className="text-muted-foreground font-normal">
                      {d.status === 'live' ? 'deployed' : d.status}
                    </span>
                  </p>
                  <p className="text-muted-foreground truncate text-[11px]">
                    {d.message || d.commit || d.trigger}
                  </p>
                </div>
                <span className="text-muted-foreground shrink-0 font-mono text-[10px] tabular-nums">
                  {rel(d.started)}
                </span>
              </button>
            ))}
          </div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
