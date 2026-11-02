import { useEffect, useState } from 'react'
import { Check } from 'lucide-react'
import { envService, type EnvVar } from '@/services/api.service'
import { Button } from '@/components/ui/button'
import { EnvFields, blankVar } from '@/components/env-fields'

export default function EnvPage() {
  const [vars, setVars] = useState<EnvVar[]>([blankVar()])
  const [saved, setSaved] = useState(false)

  useEffect(() => {
    envService
      .sharedList()
      .then((v) => setVars(v.length ? v : [blankVar()]))
      .catch(() => setVars([blankVar()]))
  }, [])

  const save = async () => {
    await envService.sharedSave(vars.filter((v) => v.key.trim()))
    setSaved(true)
  }

  return (
    <div className="px-8 py-8">
      <header className="mb-6 max-w-2xl">
        <h1 className="text-xl font-semibold tracking-tight">Environment</h1>
        <p className="text-muted-foreground mt-1 text-sm">
          Shared variables available to <b className="text-foreground/80">every project</b> in this
          team — set a value once instead of repeating it per app. A project's own variable wins if
          it defines the same key.
        </p>
      </header>

      <div className="flex flex-col gap-4">
        <EnvFields
          vars={vars}
          onChange={(v) => {
            setVars(v)
            setSaved(false)
          }}
        />

        <div className="flex items-center justify-end gap-3">
          {saved && (
            <span className="flex items-center gap-1.5 text-xs text-success">
              <Check className="h-3.5 w-3.5" /> Saved — redeploy apps to apply.
            </span>
          )}
          <Button size="sm" onClick={save}>
            Save
          </Button>
        </div>
      </div>
    </div>
  )
}
