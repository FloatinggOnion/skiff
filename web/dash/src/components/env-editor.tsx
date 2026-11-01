import { useEffect, useState } from 'react'
import { Check } from 'lucide-react'
import { envService, type EnvVar } from '@/services/api.service'
import { Button } from '@/components/ui/button'
import { EnvFields, blankVar } from '@/components/env-fields'

export function EnvEditor({ app, scope }: { app: string; scope?: 'preview' }) {
  const [vars, setVars] = useState<EnvVar[]>([blankVar()])
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    setSaved(false)
    envService
      .list(app, scope)
      .then((v) => setVars(v.length ? v : [blankVar()]))
      .catch(() => setVars([blankVar()]))
  }, [app, scope])

  const save = async () => {
    await envService.save(
      app,
      vars.filter((v) => v.key.trim()),
      scope,
    )
    setSaved(true)
  }

  return (
    <div className="flex flex-col gap-4">
      {scope === 'preview' ? (
        <p className="text-muted-foreground text-sm">
          Previews use the variables above, with these replacing any of the same name. Point{' '}
          <span className="text-foreground/70 font-mono">DATABASE_URL</span> at a staging database
          here so a preview never touches production data. Applied on each preview&apos;s next
          deploy.
        </p>
      ) : (
        <p className="text-muted-foreground text-sm">
          Injected on the next deploy. <b className="text-foreground/80">Build</b> vars are
          available during the build; the rest are runtime-only secrets, never baked into the image.
          Tip: paste a <span className="text-foreground/70 font-mono">.env</span> straight into a
          name field to fill everything at once.
        </p>
      )}

      <EnvFields
        vars={vars}
        onChange={(v) => {
          setVars(v)
          setSaved(false)
        }}
      />

      <div className="flex items-center justify-end gap-3">
        {saved && (
          <span className="flex items-center gap-1.5 text-xs text-emerald-300">
            <Check className="h-3.5 w-3.5" />{' '}
            {scope === 'preview'
              ? 'Saved — applies on each preview’s next deploy.'
              : 'Saved — redeploy to apply.'}
          </span>
        )}
        <Button size="sm" onClick={save}>
          Save
        </Button>
      </div>
    </div>
  )
}
