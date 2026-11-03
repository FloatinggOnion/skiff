import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Download, RotateCcw, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { queryKeys } from '@/constants/query-keys'
import { errText } from '@/lib/errors'
import { fmtBytes } from '@/lib/format'
import { useConfirm } from '@/providers/confirm-provider'
import { projectsService, type VolumeBackup } from '@/services/api.service'

function fmtDate(t: number) {
  return new Date(t * 1000).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function VolumeBackupsDialog({
  app,
  volume,
  open,
  onOpenChange,
}: {
  app: string
  volume: string
  open: boolean
  onOpenChange: (v: boolean) => void
}) {
  const qc = useQueryClient()
  const confirm = useConfirm()
  const [busy, setBusy] = useState('')
  const [error, setError] = useState('')
  const key = queryKeys.volumeBackups(app, volume)
  const { data: backups = [], isLoading } = useQuery<VolumeBackup[]>({
    queryKey: key,
    queryFn: () => projectsService.volumeBackups(app, volume),
    enabled: open,
  })

  const run = async (label: string, fn: () => Promise<unknown>) => {
    setError('')
    setBusy(label)
    try {
      await fn()
      qc.invalidateQueries({ queryKey: key })
    } catch (err) {
      setError(errText(err, 'Something went wrong.'))
    } finally {
      setBusy('')
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="font-mono text-sm">
            {app} / {volume} · backups
          </DialogTitle>
        </DialogHeader>
        <div className="flex items-center justify-between gap-3">
          <p className="text-muted-foreground text-xs">
            Backed up daily, keeping the last 3 here. The app pauses briefly during each copy.
          </p>
          <Button
            size="sm"
            className="shrink-0"
            disabled={busy === 'create'}
            onClick={() => run('create', () => projectsService.createVolumeBackup(app, volume))}
          >
            {busy === 'create' ? 'Backing up…' : 'Back up now'}
          </Button>
        </div>

        <div className="max-h-[50vh] divide-y divide-foreground/5 overflow-y-auto rounded-lg border border-foreground/8">
          {isLoading ? (
            <p className="text-muted-foreground p-5 text-center text-sm">Loading…</p>
          ) : backups.length === 0 ? (
            <p className="text-muted-foreground p-6 text-center text-sm">
              No backups yet. Take one now, or wait for the daily snapshot.
            </p>
          ) : (
            backups.map((b) => (
              <div key={b.id} className="flex items-center gap-2 p-3">
                <div className="min-w-0 flex-1">
                  <p className="font-mono text-xs">{fmtDate(b.created)}</p>
                  <p className="text-muted-foreground mt-0.5 text-[11px]">
                    {fmtBytes(b.size)}
                    {b.trigger === 'scheduled' && ' · auto'}
                    {b.trigger === 'pre-restore' && ' · before restore'}
                    {b.live && <span className="text-warning"> · copied while running</span>}
                    {b.offsite === 'ok' && <span className="text-success/80"> · offsite</span>}
                    {b.offsite === 'pending' && ' · copying offsite…'}
                    {b.offsite === 'failed' && (
                      <span className="text-danger"> · offsite copy failed</span>
                    )}
                  </p>
                </div>
                <a
                  href={projectsService.volumeBackupDownloadUrl(b.id)}
                  title="Download"
                  className="text-muted-foreground hover:text-foreground p-1.5 transition"
                >
                  <Download className="h-3.5 w-3.5" />
                </a>
                <button
                  onClick={async () => {
                    if (
                      await confirm({
                        title: `Restore ${volume}?`,
                        description: `This replaces its files with the ${fmtDate(b.created)} backup and restarts ${app} for a moment. Skiff backs up the current files first, so you can undo it.`,
                        confirmText: 'Restore',
                        destructive: true,
                      })
                    )
                      run('restore-' + b.id, () => projectsService.restoreVolumeBackup(b.id))
                  }}
                  disabled={busy === 'restore-' + b.id}
                  className="text-muted-foreground hover:text-foreground p-1.5 transition disabled:opacity-40"
                  title="Restore"
                >
                  <RotateCcw className="h-3.5 w-3.5" />
                </button>
                <button
                  onClick={() => run('del-' + b.id, () => projectsService.deleteVolumeBackup(b.id))}
                  disabled={busy === 'del-' + b.id}
                  className="text-muted-foreground p-1.5 transition hover:text-danger disabled:opacity-40"
                  title="Delete"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            ))
          )}
        </div>
        {error && <p className="text-xs text-danger">{error}</p>}
      </DialogContent>
    </Dialog>
  )
}
