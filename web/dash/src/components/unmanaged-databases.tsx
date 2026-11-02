import { HardDrive } from 'lucide-react'
import { errText } from '@/lib/errors'
import { useUnmanagedDatabases } from '@/hooks/use-databases'
import { Button } from '@/components/ui/button'

export function UnmanagedDatabases({ dot }: { dot: (engine: string) => string }) {
  const { found, adopt } = useUnmanagedDatabases()
  if (found.length === 0) return null

  return (
    <section className="animate-rise mb-6 rounded-xl border border-warning/20 bg-warning/[0.03] p-4">
      <h2 className="text-sm font-medium">Running on this server, not managed yet</h2>
      <p className="text-muted-foreground mt-1 text-xs">
        These were started outside Skiff, so nothing backs them up. Managing one adds daily backups,
        a shell and this dashboard view — the container keeps running untouched.
      </p>
      <div className="mt-3 divide-y divide-foreground/5">
        {found.map((d) => {
          const busy = adopt.isPending && adopt.variables?.container === d.container
          return (
            <div key={d.container} className="flex flex-wrap items-center gap-3 py-2.5">
              <span className={'h-1.5 w-1.5 shrink-0 rounded-full ' + dot(d.engine)} />
              <span className="font-mono text-sm">{d.container}</span>
              <span className="text-muted-foreground font-mono text-xs">{d.image}</span>
              {d.volume && (
                <span className="text-muted-foreground inline-flex items-center gap-1 font-mono text-[11px]">
                  <HardDrive className="h-3 w-3" />
                  {d.volume}
                </span>
              )}
              <Button
                size="sm"
                variant="outline"
                className="ml-auto"
                loading={busy}
                disabled={adopt.isPending}
                onClick={() => adopt.mutate({ container: d.container, name: d.container })}
              >
                Manage with Skiff
              </Button>
            </div>
          )
        })}
      </div>
      {adopt.isError && (
        <p className="mt-2 text-xs text-danger">{errText(adopt.error, "Couldn't adopt it.")}</p>
      )}
    </section>
  )
}
