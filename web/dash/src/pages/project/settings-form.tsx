import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { RotateCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { errText } from '@/lib/errors'
import { useConfirm } from '@/providers/confirm-provider'
import { projectsService } from '@/services/api.service'
import { projectSettingsSchema, type ProjectSettingsInput } from '@/validations/project-settings'
import { Stepper } from './stepper'

const SLEEP_OPTIONS = [
  { v: 0, label: 'Never' },
  { v: 15, label: '15m' },
  { v: 60, label: '1h' },
  { v: 360, label: '6h' },
]

// A value set through the API that isn't a preset still shows, selected.
const sleepOptions = (current: number) =>
  SLEEP_OPTIONS.some((o) => o.v === current)
    ? SLEEP_OPTIONS
    : [
        ...SLEEP_OPTIONS,
        { v: current, label: current % 60 ? `${current}m` : `${current / 60}h` },
      ].sort((a, b) => a.v - b.v)

export function SettingsForm({
  name,
  branch,
  rootDir,
  port,
  auto,
  previewAuto,
  replicas,
  running,
  release,
  autoscale,
  scaleMin,
  scaleMax,
  scaleCpu,
  sleepAfter,
  healthPath,
  healthTimeout,
  image = '',
  onSaved,
  onDeleted,
  onRedeploy,
  cli = false,
}: {
  name: string
  branch: string
  rootDir: string
  port: string
  auto: boolean
  previewAuto: boolean
  replicas: number
  running: number
  release: string
  autoscale: boolean
  scaleMin: number
  scaleMax: number
  scaleCpu: number
  sleepAfter: number
  healthPath: string
  healthTimeout: number
  image?: string
  onSaved: () => void
  onDeleted: () => void
  onRedeploy: () => void
  cli?: boolean
}) {
  const [saved, setSaved] = useState(false)
  const [saveError, setSaveError] = useState('')
  const confirm = useConfirm()
  const form = useForm<ProjectSettingsInput>({
    resolver: zodResolver(projectSettingsSchema),
    defaultValues: {
      branch,
      rootDir,
      port,
      auto,
      previewAuto,
      replicas: replicas || 1,
      release: release || '',
      autoscale,
      scaleMin: scaleMin || 1,
      scaleMax: scaleMax || Math.max(scaleMin || 1, replicas || 2),
      scaleCpu: scaleCpu || 70,
      sleepAfter: sleepAfter || 0,
      healthPath: healthPath || '',
      healthTimeout: healthTimeout || 0,
      image,
    },
  })
  const { register, handleSubmit, watch, setValue, formState } = form
  const values = watch()
  const fromImage = !!image
  const setReplicas = (n: number) =>
    setValue('replicas', Math.min(10, Math.max(1, n)), { shouldDirty: true })
  const setNum = (field: 'scaleMin' | 'scaleMax', n: number) =>
    setValue(field, Math.min(10, Math.max(1, n)), { shouldDirty: true })
  const toggleClass = (on: boolean) =>
    'h-9 rounded-md border px-3 font-mono text-xs uppercase transition-colors ' +
    (on
      ? 'border-success/30 bg-success/10 text-success'
      : 'border-foreground/15 text-muted-foreground')

  const save = handleSubmit(async (data) => {
    setSaveError('')
    try {
      await projectsService.update(name, data)
      setSaved(true)
      onSaved()
    } catch (err) {
      setSaved(false)
      setSaveError(errText(err, 'Could not save the settings.'))
    }
  })

  const del = async () => {
    if (
      !(await confirm({
        title: `Delete ${name}?`,
        description: 'This stops the app and deletes its config and volumes.',
        confirmText: 'Delete',
        destructive: true,
      }))
    )
      return
    await projectsService.stop(name)
    onDeleted()
  }

  return (
    <div className="flex flex-col gap-6">
      <section className="rounded-xl border border-foreground/8 p-5">
        <h2 className="mb-4 text-sm font-semibold">Build &amp; deploy</h2>
        <FieldGroup className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {fromImage && (
            <Field data-invalid={!!formState.errors.image} className="sm:col-span-2">
              <FieldLabel htmlFor="s-image">Image</FieldLabel>
              <Input
                id="s-image"
                placeholder="ghcr.io/you/app:latest"
                className="font-mono"
                aria-invalid={!!formState.errors.image}
                {...register('image')}
              />
              <FieldError errors={[formState.errors.image]} />
            </Field>
          )}
          {!cli && !fromImage && (
            <>
              <Field>
                <FieldLabel htmlFor="s-branch">Branch</FieldLabel>
                <Input
                  id="s-branch"
                  placeholder="main"
                  className="font-mono"
                  {...register('branch')}
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="s-root">Root directory</FieldLabel>
                <Input id="s-root" placeholder="/" className="font-mono" {...register('rootDir')} />
              </Field>
            </>
          )}
          <Field data-invalid={!!formState.errors.port}>
            <FieldLabel htmlFor="s-port">Port</FieldLabel>
            <Input
              id="s-port"
              placeholder={fromImage ? 'From the image' : '3000'}
              className="font-mono"
              aria-invalid={!!formState.errors.port}
              {...register('port')}
            />
            <FieldError errors={[formState.errors.port]} />
          </Field>
          {!cli && !fromImage && (
            <>
              <Field>
                <FieldLabel htmlFor="s-auto">Auto-deploy on push</FieldLabel>
                <button
                  id="s-auto"
                  type="button"
                  onClick={() => setValue('auto', !values.auto, { shouldDirty: true })}
                  className={toggleClass(values.auto)}
                >
                  {values.auto ? 'On' : 'Off'}
                </button>
              </Field>
              <Field>
                <FieldLabel htmlFor="s-preview">Preview deployments</FieldLabel>
                <button
                  id="s-preview"
                  type="button"
                  onClick={() =>
                    setValue('previewAuto', !values.previewAuto, { shouldDirty: true })
                  }
                  className={toggleClass(values.previewAuto)}
                >
                  {values.previewAuto ? 'On' : 'Off'}
                </button>
              </Field>
            </>
          )}
          {!values.autoscale && (
            <Field>
              <FieldLabel>Replicas</FieldLabel>
              <Stepper
                value={values.replicas}
                onDec={() => setReplicas(values.replicas - 1)}
                onInc={() => setReplicas(values.replicas + 1)}
              />
            </Field>
          )}
        </FieldGroup>
        <p className="text-muted-foreground mt-3 text-xs">
          Replicas run identical copies of the app behind the router, sharing traffic.
          {cli ? (
            <>
              {' '}
              The next <span className="text-foreground/70 font-mono">skiff deploy</span> uses the{' '}
              <span className="text-foreground/70 font-mono">replicas</span> in its{' '}
              <span className="text-foreground/70 font-mono">skiff.toml</span>.
            </>
          ) : fromImage ? (
            <>
              {' '}
              Redeploy pulls the image again, so a moved tag like{' '}
              <span className="text-foreground/70 font-mono">latest</span> picks up its new
              version.
            </>
          ) : (
            <>
              {' '}
              Preview deployments spin up an environment automatically for any push to a branch
              other than{' '}
              <span className="text-foreground/70 font-mono">{values.branch || 'main'}</span>.
            </>
          )}
        </p>

        <div className="mt-4 rounded-lg border border-foreground/8 p-4">
          <div className="flex items-start justify-between gap-4">
            <div>
              <div className="flex items-center gap-2">
                <h3 className="text-sm font-medium">Autoscaling</h3>
                {values.autoscale && (
                  <span className="text-muted-foreground rounded bg-foreground/5 px-1.5 py-0.5 font-mono text-[10px] tabular-nums">
                    {running} running
                  </span>
                )}
              </div>
              <p className="text-muted-foreground mt-0.5 text-xs">
                Add and retire replicas automatically to hold each one near a target CPU.
              </p>
            </div>
            <button
              type="button"
              onClick={() => setValue('autoscale', !values.autoscale, { shouldDirty: true })}
              className={toggleClass(values.autoscale)}
            >
              {values.autoscale ? 'On' : 'Off'}
            </button>
          </div>
          {values.autoscale && (
            <FieldGroup className="mt-4 grid gap-4 sm:grid-cols-3">
              <Field>
                <FieldLabel>Min replicas</FieldLabel>
                <Stepper
                  value={values.scaleMin}
                  onDec={() => setNum('scaleMin', values.scaleMin - 1)}
                  onInc={() => setNum('scaleMin', values.scaleMin + 1)}
                />
              </Field>
              <Field data-invalid={!!formState.errors.scaleMax}>
                <FieldLabel>Max replicas</FieldLabel>
                <Stepper
                  value={values.scaleMax}
                  onDec={() => setNum('scaleMax', values.scaleMax - 1)}
                  onInc={() => setNum('scaleMax', values.scaleMax + 1)}
                />
                <FieldError errors={[formState.errors.scaleMax]} />
              </Field>
              <Field data-invalid={!!formState.errors.scaleCpu}>
                <FieldLabel htmlFor="s-target">Target CPU %</FieldLabel>
                <Input
                  id="s-target"
                  type="number"
                  min={10}
                  max={100}
                  step={5}
                  className="font-mono"
                  aria-invalid={!!formState.errors.scaleCpu}
                  {...register('scaleCpu', { valueAsNumber: true })}
                />
                <FieldError errors={[formState.errors.scaleCpu]} />
              </Field>
            </FieldGroup>
          )}
        </div>
        <div className="mt-4 flex items-start justify-between gap-4 rounded-lg border border-foreground/8 p-4">
          <div>
            <h3 className="text-sm font-medium">Sleep when idle</h3>
            <p className="text-muted-foreground mt-0.5 text-xs">
              {values.autoscale
                ? 'Not available while autoscaling is on.'
                : 'Stop the app after a stretch with no visitors, freeing its memory. The next visit wakes it — that first request waits a few seconds while it starts.'}
            </p>
          </div>
          <div className="flex shrink-0 rounded-md border border-foreground/12 p-0.5">
            {sleepOptions(values.sleepAfter).map((o) => (
              <button
                key={o.v}
                type="button"
                disabled={values.autoscale}
                onClick={() => setValue('sleepAfter', o.v, { shouldDirty: true })}
                className={
                  'rounded px-2.5 py-1 font-mono text-[11px] transition-colors disabled:opacity-40 ' +
                  (values.sleepAfter === o.v
                    ? 'bg-foreground/10 text-foreground'
                    : 'text-muted-foreground hover:text-foreground')
                }
              >
                {o.label}
              </button>
            ))}
          </div>
        </div>
        {!cli && (
          <Field className="mt-4">
            <FieldLabel htmlFor="s-release">Release command</FieldLabel>
            <Input
              id="s-release"
              placeholder="e.g. npm run migrate"
              className="font-mono"
              {...register('release')}
            />
            <FieldDescription>
              Runs once in a one-off container before each new version goes live — a non-zero exit
              aborts the deploy, so the old version keeps serving.
            </FieldDescription>
          </Field>
        )}
        {!cli && (
          <div className="mt-4 grid gap-4 sm:grid-cols-[1fr_10rem]">
            <Field data-invalid={!!formState.errors.healthPath}>
              <FieldLabel htmlFor="s-health">Health check path</FieldLabel>
              <Input
                id="s-health"
                placeholder="e.g. /healthz"
                className="font-mono"
                aria-invalid={!!formState.errors.healthPath}
                {...register('healthPath')}
              />
              <FieldError errors={[formState.errors.healthPath]} />
              <FieldDescription>
                A new version takes traffic only once this answers 2xx or 3xx. Left empty, any
                response from the app counts.
              </FieldDescription>
            </Field>
            <Field data-invalid={!!formState.errors.healthTimeout}>
              <FieldLabel htmlFor="s-health-timeout">Timeout (seconds)</FieldLabel>
              <Input
                id="s-health-timeout"
                type="number"
                min={0}
                max={600}
                placeholder="30"
                className="font-mono"
                aria-invalid={!!formState.errors.healthTimeout}
                {...register('healthTimeout', {
                  setValueAs: (v) => (v === '' || v === null ? 0 : Number(v)),
                })}
              />
              <FieldError errors={[formState.errors.healthTimeout]} />
              <FieldDescription>0 uses the default, 30.</FieldDescription>
            </Field>
          </div>
        )}
        <div className="mt-5 flex items-center justify-end gap-3">
          {saveError && <span className="text-xs text-danger">{saveError}</span>}
          {saved && <span className="text-muted-foreground text-xs">Saved.</span>}
          {!cli && (
            <Button size="sm" variant="outline" onClick={onRedeploy}>
              <RotateCw />
              Redeploy
            </Button>
          )}
          <Button size="sm" onClick={save}>
            Save changes
          </Button>
        </div>
      </section>

      <section className="rounded-xl border border-danger/20 bg-danger/[0.03] p-5">
        <h2 className="mb-1 text-sm font-semibold text-danger">Danger zone</h2>
        <p className="text-muted-foreground mb-4 text-sm">
          Stop this app and remove its configuration. This cannot be undone.
        </p>
        <Button
          size="sm"
          variant="outline"
          onClick={del}
          className="border-danger/30 text-danger hover:bg-danger/10 hover:text-danger"
        >
          Delete project
        </Button>
      </section>
    </div>
  )
}
