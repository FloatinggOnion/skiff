import { Check, Copy, Download, KeyRound } from 'lucide-react'
import { Link } from 'react-router'
import { fmtBytes, relTime } from '@/lib/format'
import { errText } from '@/lib/errors'
import { useCopy } from '@/hooks/use-copy'
import { useControlPlane } from '@/hooks/use-server'
import { Button } from '@/components/ui/button'

export function ControlPlaneBackup() {
  const { query, backup, reveal, downloadUrl } = useControlPlane()
  const { copied, copy } = useCopy()
  const cp = query.data
  if (!cp) return null
  const last = cp.backups[0]
  const key = reveal.data?.recoveryKey

  return (
    <section className="mt-6">
      <h2 className="text-muted-foreground mb-3 font-mono text-[11px] tracking-wider uppercase">
        Control-plane backup
      </h2>
      <div className="rounded-xl border border-foreground/8 bg-linear-to-b from-foreground/2 to-transparent p-5">
        <p className="text-muted-foreground text-sm leading-relaxed">
          Skiff&apos;s own state — projects, settings, domains, tokens and history — sealed with
          your recovery key. To move to a new server, run{' '}
          <span className="text-foreground/80 font-mono text-xs">
            skiff server restore &lt;file&gt; --key …
          </span>{' '}
          there; databases come back from their offsite backups on their own.
        </p>

        <div className="mt-4 flex flex-wrap items-center gap-x-4 gap-y-2 text-xs">
          {cp.bucket ? (
            <span className="text-muted-foreground">
              Daily to <span className="text-foreground/80 font-mono">{cp.bucket}</span>
              {last ? ` · last ${relTime(last.created)} (${fmtBytes(last.size)})` : ' · none yet'}
            </span>
          ) : (
            <span className="text-warning/90">
              No offsite bucket yet — add one in{' '}
              <Link to="/settings" className="underline">
                Settings
              </Link>{' '}
              for daily copies. You can still download one now.
            </span>
          )}
        </div>

        <div className="mt-4 flex flex-wrap items-center gap-2">
          {cp.bucket && (
            <Button size="sm" loading={backup.isPending} onClick={() => backup.mutate()}>
              Back up now
            </Button>
          )}
          <Button size="sm" variant="outline" asChild>
            <a href={downloadUrl}>
              <Download />
              Download
            </a>
          </Button>
          {!key && (
            <Button
              size="sm"
              variant="outline"
              loading={reveal.isPending}
              onClick={() => reveal.mutate()}
            >
              <KeyRound />
              Show recovery key
            </Button>
          )}
        </div>
        {(backup.isError || reveal.isError) && (
          <p className="mt-2 text-xs text-danger">
            {errText(backup.error ?? reveal.error, 'Something went wrong.')}
          </p>
        )}

        {key && (
          <div className="mt-4 rounded-lg border border-warning/20 bg-warning/[0.04] p-3">
            <div className="flex items-center gap-2">
              <code className="min-w-0 flex-1 truncate font-mono text-xs">{key}</code>
              <button
                onClick={() => copy(key)}
                className="text-muted-foreground hover:text-foreground shrink-0 p-1"
                aria-label="Copy recovery key"
              >
                {copied ? <Check className="h-3.5 w-3.5" /> : <Copy className="h-3.5 w-3.5" />}
              </button>
            </div>
            <p className="text-muted-foreground mt-2 text-[11px]">
              Keep this in a password manager, away from this server. Without it, backups can&apos;t
              be opened.
            </p>
          </div>
        )}
      </div>
    </section>
  )
}
