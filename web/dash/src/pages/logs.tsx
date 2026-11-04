import { useEffect, useMemo } from 'react'
import { useSearchParams } from 'react-router'
import { Trash2, ScrollText } from 'lucide-react'
import { LogHistory } from '@/components/log-history'
import { useApps } from '@/hooks/use-apps'
import { useLogStream } from '@/hooks/use-log-stream'
import { useUrlState } from '@/hooks/use-url-state'
import { useAutoScroll } from '@/hooks/use-auto-scroll'

const runningDot = (state: string): string =>
  state === 'running'
    ? 'bg-success'
    : state === 'exited' || state === 'missing'
      ? 'bg-danger'
      : 'bg-warning'

export default function LogsPage() {
  const { apps } = useApps()
  const [params] = useSearchParams()
  const [appParam, setSelected] = useUrlState('app')
  const selected = appParam || null
  const window = useMemo(() => {
    const since = Number(params.get('since')),
      until = Number(params.get('until'))
    return since > 0 && until > since ? { since, until } : undefined
  }, [params])
  const [modeParam, setMode] = useUrlState('mode', window ? 'history' : 'live')
  const mode = modeParam === 'history' ? 'history' : 'live'

  // Default to the first app once they load; keep the selection valid.
  useEffect(() => {
    if (apps.length === 0) return
    if (!apps.some((a) => a.name === appParam)) setSelected(apps[0].name)
  }, [apps, appParam, setSelected])

  const { lines, live, clear } = useLogStream(mode === 'live' ? selected : null)
  const scrollRef = useAutoScroll<HTMLDivElement>(lines.length)

  return (
    <div className="flex h-[calc(100svh-3.5rem)] flex-col overflow-hidden px-8 py-8">
      <header className="mb-6 shrink-0">
        <h1 className="text-xl font-semibold tracking-tight">Logs</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Live output from the app and its workers, and the last 7 days saved and searchable.
        </p>
      </header>

      <div className="flex min-h-0 flex-1 gap-5">
        {/* app picker */}
        <aside className="w-52 shrink-0">
          <p className="text-muted-foreground mb-2 px-1 font-mono text-[10px] tracking-wider uppercase">
            Apps
          </p>
          {apps.length === 0 ? (
            <p className="text-muted-foreground px-1 text-xs">No apps deployed.</p>
          ) : (
            <div className="flex flex-col gap-0.5">
              {apps.map((a) => (
                <button
                  key={a.name}
                  onClick={() => setSelected(a.name)}
                  className={
                    'flex items-center gap-2.5 rounded-[6px] px-2.5 py-1.5 text-left text-sm transition-colors ' +
                    (selected === a.name
                      ? 'text-foreground bg-foreground/8'
                      : 'text-muted-foreground hover:text-foreground hover:bg-foreground/3')
                  }
                >
                  <span className={'h-1.5 w-1.5 shrink-0 rounded-full ' + runningDot(a.state)} />
                  <span className="truncate font-mono text-xs">{a.name}</span>
                </button>
              ))}
            </div>
          )}
        </aside>

        {/* terminal */}
        <div className="dark bg-well text-foreground flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-xl">
          <div className="border-foreground/6 flex h-10 shrink-0 items-center gap-3 border-b px-4">
            <div className="flex items-center gap-1.5">
              <span className="border-foreground/15 h-2.5 w-2.5 rounded-full border" />
              <span className="border-foreground/15 h-2.5 w-2.5 rounded-full border" />
              <span className="border-foreground/15 h-2.5 w-2.5 rounded-full border" />
            </div>
            <span className="text-muted-foreground font-mono text-xs">
              {selected ? selected : 'no app selected'}
            </span>
            <div className="border-foreground/10 flex rounded-[5px] border p-0.5">
              {(['live', 'history'] as const).map((m) => (
                <button
                  key={m}
                  onClick={() => setMode(m)}
                  className={
                    'rounded-[3px] px-2 py-0.5 font-mono text-[11px] ' +
                    (mode === m ? 'bg-foreground/10 text-foreground' : 'text-muted-foreground')
                  }
                >
                  {m === 'live' ? 'Live' : 'History'}
                </button>
              ))}
            </div>
            {selected && mode === 'live' && (
              <span className="flex items-center gap-1.5 font-mono text-[11px]">
                <span
                  className={
                    'h-1.5 w-1.5 rounded-full ' +
                    (live ? 'pulse-dot bg-success' : 'bg-foreground/25')
                  }
                />
                <span className={live ? 'text-success/80' : 'text-muted-foreground'}>
                  {live ? 'live' : 'ended'}
                </span>
              </span>
            )}
            <div className={'ml-auto flex items-center gap-3 ' + (mode === 'live' ? '' : 'hidden')}>
              <span className="text-muted-foreground font-mono text-[11px] tabular-nums">
                {lines.length} lines
              </span>
              <button
                onClick={clear}
                disabled={lines.length === 0}
                className="text-muted-foreground hover:text-foreground flex items-center gap-1 text-xs transition-colors disabled:opacity-30"
              >
                <Trash2 className="h-3.5 w-3.5" />
                Clear
              </button>
            </div>
          </div>

          {mode === 'history' && selected && <LogHistory app={selected} window={window} />}
          <div
            ref={scrollRef}
            hidden={mode === 'history' && !!selected}
            className="term-scroll min-h-0 flex-1 overflow-auto px-4 py-3 font-mono text-xs leading-relaxed"
          >
            {!selected ? (
              <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-2">
                <ScrollText className="h-6 w-6 opacity-40" />
                <span>Select an app to stream its logs.</span>
              </div>
            ) : lines.length === 0 ? (
              <p className="text-muted-foreground">Waiting for output…</p>
            ) : (
              lines.map((line, i) => (
                <div key={i} className="text-foreground/70 wrap-break-word whitespace-pre-wrap">
                  {line || ' '}
                </div>
              ))
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
