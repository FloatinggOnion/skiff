import { relTime } from '@/lib/format'
import type { Deploy } from '@/services/api.service'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'

const statusDot = (s: string): string =>
  s === 'live' ? 'bg-success' : s === 'failed' ? 'bg-danger' : 'bg-warning'

export function DeployHistory({
  app,
  deploys,
  onClose,
  onViewLog,
}: {
  app: string | null
  deploys: Deploy[]
  onClose: () => void
  onViewLog: (app: string, id: string) => void
}) {
  return (
    <Dialog open={!!app} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            Deploys · <span className="font-mono">{app}</span>
          </DialogTitle>
        </DialogHeader>
        <div className="max-h-80 overflow-auto rounded-md border">
          {deploys.length === 0 ? (
            <p className="text-muted-foreground p-6 text-center text-sm">No deploys yet.</p>
          ) : (
            deploys.map((d) => (
              <button
                key={d.id}
                onClick={() => app && onViewLog(app, d.id)}
                className="hover:bg-accent flex w-full items-center gap-3 border-b px-4 py-3 text-left last:border-0"
              >
                <span className={'h-1.5 w-1.5 shrink-0 rounded-full ' + statusDot(d.status)} />
                <span className="min-w-0 flex-1 truncate text-xs">
                  {d.message || d.commit || d.status}
                </span>
                {d.commit && (
                  <span className="text-muted-foreground shrink-0 font-mono text-[11px]">
                    {d.commit}
                  </span>
                )}
                <span className="text-muted-foreground shrink-0 font-mono text-[11px]">
                  {d.trigger} · {relTime(d.started)}
                </span>
              </button>
            ))
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
