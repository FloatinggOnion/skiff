import { useEffect, useState } from 'react'
import { lineClass } from '@/components/drawer'
import { useAutoScroll } from '@/hooks/use-auto-scroll'
import { deployLogUrl, openStream } from '@/services/stream.service'

// Below the waterline: the build log of the project's latest deploy, live while it's running.
export function DeployLogPanel({ app, id }: { app: string; id: string }) {
  const [lines, setLines] = useState<string[]>([])
  const ref = useAutoScroll<HTMLDivElement>(lines.length)

  useEffect(() => {
    setLines([])
    const es = openStream(deployLogUrl(app, id), {
      onLine: (l) => setLines((prev) => [...prev, l]),
      onDone: () => {},
    })
    return () => es.close()
  }, [app, id])

  return (
    <div ref={ref} className="term-scroll max-h-[60vh] overflow-auto font-mono text-xs leading-relaxed">
      {lines.length === 0 ? (
        <p className="text-foreground/50">No log saved for this deploy.</p>
      ) : (
        lines.map((l, i) => (
          <div key={i} className={'whitespace-pre-wrap ' + lineClass(l)}>
            {l || ' '}
          </div>
        ))
      )}
    </div>
  )
}
