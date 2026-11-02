import { useEffect, useState } from 'react'
import { X, Square } from 'lucide-react'
import { useAutoScroll } from '@/hooks/use-auto-scroll'
import type { Stream } from '@/hooks/use-console'

// Colour build/log lines by their leading glyph, matching the panel's output.
function lineClass(line: string): string {
  const t = line.trimStart()
  if (t.startsWith('✓')) return 'text-success'
  if (t.startsWith('✗')) return 'text-danger'
  if (t.startsWith('→')) return 'text-info/80'
  return 'text-foreground/70'
}

export function Drawer({
  stream,
  onClose,
  onStop,
}: {
  stream: Stream
  onClose: () => void
  onStop?: () => void
}) {
  const ref = useAutoScroll<HTMLDivElement>(stream.lines.length)
  const [stopping, setStopping] = useState(false)

  const last = stream.lines[stream.lines.length - 1] ?? ''
  const finished = last.startsWith('✓ done') || last.startsWith('✗ failed')
  const canStop = !!stream.app && !!onStop && !finished

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [onClose])

  return (
    <div className="animate-drawer-up fixed inset-x-0 bottom-0 z-50 flex h-[44vh] flex-col border-t-2 border-seam dark bg-well text-foreground shadow-[0_-24px_70px_-24px_rgba(0,0,0,0.95)] backdrop-blur-md">
      <div className="flex h-10 shrink-0 items-center gap-3 border-b border-foreground/6 px-4">
        <div className="flex items-center gap-1.5">
          <span className="h-2.5 w-2.5 rounded-full border border-foreground/15" />
          <span className="h-2.5 w-2.5 rounded-full border border-foreground/15" />
          <span className="h-2.5 w-2.5 rounded-full border border-foreground/15" />
        </div>
        <span className="text-muted-foreground flex items-center gap-1.5 font-mono text-xs">
          <span className="text-foreground/30">▸</span>
          {stream.title}
        </span>
        <div className="flex-1" />
        {canStop && (
          <button
            onClick={() => {
              setStopping(true)
              onStop?.()
            }}
            disabled={stopping}
            className="flex items-center gap-1.5 rounded-md border border-danger/30 px-2 py-1 font-mono text-[11px] text-danger transition-colors hover:bg-danger/10 disabled:opacity-50"
          >
            <Square className="h-3 w-3 fill-current" />
            {stopping ? 'stopping…' : 'stop'}
          </button>
        )}
        <button
          onClick={onClose}
          className="text-muted-foreground hover:text-foreground flex items-center gap-1.5 rounded-md px-2 py-1 font-mono text-[11px] transition-colors hover:bg-foreground/5"
        >
          <X className="h-3.5 w-3.5" />
          esc
        </button>
      </div>
      <div
        ref={ref}
        className="term-scroll flex-1 overflow-auto px-5 py-4 font-mono text-[12.5px] leading-[1.7]"
      >
        {stream.lines.length === 0 ? (
          <div className="text-foreground/30">connecting…</div>
        ) : (
          stream.lines.map((line, i) => (
            <div key={i} className={'break-words whitespace-pre-wrap ' + lineClass(line)}>
              {line || ' '}
            </div>
          ))
        )}
      </div>
    </div>
  )
}
