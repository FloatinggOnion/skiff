import { useEffect, useMemo, useState } from 'react'
import { Search, ScrollText } from 'lucide-react'
import { useLogHistory } from '@/hooks/use-log-history'

const RANGES = [
  { label: '1h', secs: 3600 },
  { label: '24h', secs: 86400 },
  { label: '7d', secs: 7 * 86400 },
]

const fmtTime = (iso: string, withDay: boolean) => {
  const d = new Date(iso)
  return withDay
    ? d.toLocaleString([], {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
      })
    : d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

function Highlight({ text, q }: { text: string; q: string }) {
  if (!q) return <>{text}</>
  const i = text.toLowerCase().indexOf(q.toLowerCase())
  if (i < 0) return <>{text}</>
  return (
    <>
      {text.slice(0, i)}
      <mark className="rounded-[2px] bg-warning/25 text-warning">
        {text.slice(i, i + q.length)}
      </mark>
      {text.slice(i + q.length)}
    </>
  )
}

// Saved logs for one app: search, errors-only, and either a recent range or a fixed window (e.g. after a deploy).
export function LogHistory({
  app,
  window,
}: {
  app: string
  window?: { since: number; until: number }
}) {
  const [input, setInput] = useState('')
  const [q, setQ] = useState('')
  const [errors, setErrors] = useState(false)
  const [rangeSecs, setRangeSecs] = useState(86400)
  const [fixed, setFixed] = useState(window)

  useEffect(() => setFixed(window), [window])
  useEffect(() => {
    const t = setTimeout(() => setQ(input.trim()), 300)
    return () => clearTimeout(t)
  }, [input])

  const [now] = useState(() => Math.floor(Date.now() / 1000))
  const since = fixed ? fixed.since : now - rangeSecs
  const until = fixed ? fixed.until : 0
  const { data, isPending, isError } = useLogHistory(app, q, errors, since, until)
  const lines = useMemo(() => data?.lines ?? [], [data])
  const multiDay = fixed ? fixed.until - fixed.since > 86400 : rangeSecs > 86400
  const containers = new Set(lines.map((l) => l.container)).size

  return (
    <>
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b border-foreground/6 px-3 py-2">
        <div className="relative min-w-40 flex-1">
          <Search className="text-muted-foreground pointer-events-none absolute top-1/2 left-2 h-3.5 w-3.5 -translate-y-1/2" />
          <input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="Search logs…"
            className="h-7 w-full rounded-[5px] border border-foreground/10 bg-inset pr-2 pl-7 font-mono text-xs outline-none placeholder:text-foreground/25 focus-visible:border-foreground/25"
          />
        </div>
        <button
          onClick={() => setErrors((e) => !e)}
          className={
            'h-7 rounded-[5px] border px-2.5 font-mono text-[11px] transition-colors ' +
            (errors
              ? 'border-danger/30 bg-danger/10 text-danger'
              : 'text-muted-foreground border-foreground/10 hover:text-foreground')
          }
        >
          Errors only
        </button>
        {fixed ? (
          <button
            onClick={() => setFixed(undefined)}
            className="text-muted-foreground h-7 rounded-[5px] border border-foreground/10 px-2.5 font-mono text-[11px] hover:text-foreground"
            title="Show recent logs instead"
          >
            {fmtTime(new Date(fixed.since * 1000).toISOString(), true)} →{' '}
            {fmtTime(new Date(fixed.until * 1000).toISOString(), true)} ✕
          </button>
        ) : (
          <div className="flex rounded-[5px] border border-foreground/10 p-0.5">
            {RANGES.map((r) => (
              <button
                key={r.label}
                onClick={() => setRangeSecs(r.secs)}
                className={
                  'rounded-[3px] px-2 py-0.5 font-mono text-[11px] ' +
                  (rangeSecs === r.secs ? 'bg-foreground/10 text-foreground' : 'text-muted-foreground')
                }
              >
                {r.label}
              </button>
            ))}
          </div>
        )}
      </div>
      <div className="term-scroll min-h-0 flex-1 overflow-auto px-4 py-3 font-mono text-xs leading-relaxed">
        {isPending ? (
          <p className="text-muted-foreground">Searching…</p>
        ) : isError ? (
          <p className="text-danger">Couldn&apos;t read saved logs.</p>
        ) : lines.length === 0 ? (
          <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-2">
            <ScrollText className="h-6 w-6 opacity-40" />
            <span>{q || errors ? 'No matching lines.' : 'No logs saved for this range yet.'}</span>
          </div>
        ) : (
          <>
            {data?.truncated && (
              <p className="text-muted-foreground mb-2">
                Showing the newest {lines.length} lines — narrow the search to see older ones.
              </p>
            )}
            {lines.map((l, i) => (
              <div key={i} className="flex gap-3 whitespace-pre-wrap">
                <span className="shrink-0 text-foreground/30 tabular-nums">
                  {fmtTime(l.t, multiDay)}
                </span>
                {containers > 1 && (
                  <span className="w-24 shrink-0 truncate text-foreground/25">{l.container}</span>
                )}
                <span
                  className={
                    'min-w-0 wrap-break-word ' +
                    (l.stream === 'err' ? 'text-danger/80' : 'text-foreground/70')
                  }
                >
                  <Highlight text={l.text} q={q} />
                </span>
              </div>
            ))}
          </>
        )}
      </div>
    </>
  )
}
