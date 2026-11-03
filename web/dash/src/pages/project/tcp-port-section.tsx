import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { queryKeys } from '@/constants/query-keys'
import { errText } from '@/lib/errors'
import { useConfirm } from '@/providers/confirm-provider'
import { projectsService } from '@/services/api.service'

export function TCPPortSection({ app }: { app: string }) {
  const qc = useQueryClient()
  const confirm = useConfirm()
  const key = queryKeys.tcpPort(app)
  const { data } = useQuery({ queryKey: key, queryFn: () => projectsService.tcpPort(app) })
  const [value, setValue] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const save = async (port: number) => {
    setError('')
    setBusy(true)
    try {
      await projectsService.setTCPPort(app, port)
      setValue('')
      qc.invalidateQueries({ queryKey: key })
    } catch (err) {
      setError(errText(err, 'Could not change the port.'))
    } finally {
      setBusy(false)
    }
  }

  const open = async (e: FormEvent) => {
    e.preventDefault()
    const port = Number(value)
    if (!Number.isInteger(port) || port < 1024 || port > 65535) {
      setError('Use a port from 1024 to 65535.')
      return
    }
    if (
      await confirm({
        title: `Open port ${port} to the internet?`,
        description: `Anyone who can reach your server can connect to ${app} on this port, so make sure the service has its own password. You may also need to allow the port in your provider's firewall.`,
        confirmText: 'Open port',
        destructive: true,
      })
    )
      void save(port)
  }

  return (
    <section className="flex flex-col gap-3">
      <div>
        <h2 className="text-sm font-semibold">Public TCP port</h2>
        <p className="text-muted-foreground mt-1 text-xs">
          For services that don't speak HTTP, like game servers or message brokers. Skiff forwards this port to
          port {data?.containerPort ?? '…'} of your app, and it keeps working across deploys.
        </p>
      </div>
      {data?.port ? (
        <div className="flex items-center justify-between gap-3 rounded-xl border border-foreground/8 p-4">
          <p className="font-mono text-sm">Open on port {data.port}</p>
          <Button size="sm" variant="outline" loading={busy} onClick={() => void save(0)}>
            Close port
          </Button>
        </div>
      ) : (
        <form onSubmit={open} className="flex items-start gap-2">
          <Input
            aria-label="Public TCP port"
            inputMode="numeric"
            className="font-mono sm:max-w-40"
            placeholder="25565"
            value={value}
            onChange={(e) => setValue(e.target.value)}
          />
          <Button size="sm" type="submit" variant="outline" loading={busy} disabled={!value}>
            Open port
          </Button>
        </form>
      )}
      {error && <p className="text-xs text-danger">{error}</p>}
    </section>
  )
}
