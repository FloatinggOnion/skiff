import { useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ExternalLink } from 'lucide-react'
import { useApps } from '@/hooks/use-apps'
import { queryKeys } from '@/constants/query-keys'
import { errText } from '@/lib/errors'
import { projectsService } from '@/services/api.service'
import { Section, Field, SaveButton, inputCls } from './ui'

export function StatusPageSection() {
  const qc = useQueryClient()
  const { apps } = useApps()
  const { data } = useQuery({
    queryKey: queryKeys.statusPage,
    queryFn: () => projectsService.statusPage(),
  })
  const [enabled, setEnabled] = useState(false)
  const [title, setTitle] = useState('')
  const [host, setHost] = useState('')
  const [selected, setSelected] = useState<string[]>([])
  const hydrated = useRef(false)

  useEffect(() => {
    if (data && !hydrated.current) {
      setEnabled(data.enabled)
      setTitle(data.title)
      setHost(data.host)
      setSelected(data.apps)
      hydrated.current = true
    }
  }, [data])

  const save = useMutation({
    mutationFn: () => projectsService.saveStatusPage({ enabled, title, host, apps: selected }),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.statusPage }),
  })
  const change = <T,>(set: (v: T) => void) => (v: T) => {
    set(v)
    save.reset()
  }
  const toggle = (app: string) =>
    change(setSelected)(selected.includes(app) ? selected.filter((a) => a !== app) : [...selected, app])

  const live = !!data?.enabled && !!data.slug

  return (
    <Section
      title="Public status page"
      description="A page anyone can open, with the current state and 30 days of uptime for the projects you choose. Nothing is public until you turn it on, and error details are never shown."
    >
      <form
        onSubmit={(e) => {
          e.preventDefault()
          save.mutate()
        }}
        className="space-y-3"
      >
        <label className="flex items-center gap-2.5 text-sm">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => change(setEnabled)(e.target.checked)}
          />
          Publish the status page
        </label>
        <Field label="Title">
          <input
            className={inputCls}
            placeholder="Acme status"
            maxLength={80}
            value={title}
            onChange={(e) => change(setTitle)(e.target.value)}
          />
        </Field>
        <div className="grid gap-1.5">
          <span className="text-sm font-medium">Projects to show</span>
          {apps.length === 0 ? (
            <p className="text-muted-foreground text-xs">Deploy a project first.</p>
          ) : (
            <div className="grid gap-1 sm:grid-cols-2">
              {apps.map((a) => (
                <label key={a.name} className="flex min-w-0 items-center gap-2 text-sm">
                  <input
                    type="checkbox"
                    checked={selected.includes(a.name)}
                    onChange={() => toggle(a.name)}
                  />
                  <span className="truncate font-mono text-xs">{a.name}</span>
                </label>
              ))}
            </div>
          )}
        </div>
        <Field label="Custom domain (optional)">
          <input
            className={inputCls + ' font-mono'}
            placeholder="status.example.com"
            value={host}
            onChange={(e) => change(setHost)(e.target.value)}
          />
        </Field>
        {save.error ? (
          <p className="text-danger text-xs">{errText(save.error, 'Could not save the status page.')}</p>
        ) : (
          host && (
            <p className="text-muted-foreground text-xs">
              Point this domain&apos;s DNS at your server. The certificate is issued automatically.
            </p>
          )
        )}
        <div className="flex items-center justify-end gap-3">
          {live && (
            <a
              href={data!.url}
              target="_blank"
              rel="noreferrer"
              className="text-muted-foreground hover:text-foreground mr-auto flex min-w-0 items-center gap-1.5 text-xs"
            >
              <ExternalLink className="h-3.5 w-3.5 shrink-0" />
              <span className="truncate font-mono">{data!.url.replace('https://', '')}</span>
            </a>
          )}
          <SaveButton busy={save.isPending} saved={save.isSuccess} disabled={false} />
        </div>
      </form>
    </Section>
  )
}
