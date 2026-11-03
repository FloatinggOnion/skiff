import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Archive, HardDrive, Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { queryKeys } from '@/constants/query-keys'
import { errText } from '@/lib/errors'
import { fmtBytes } from '@/lib/format'
import { useConfirm } from '@/providers/confirm-provider'
import { projectsService, type Volumes } from '@/services/api.service'
import { volumeSchema, type VolumeInput } from '@/validations/volume'
import { VolumeBackupsDialog } from './volume-backups-dialog'

export function VolumesPanel({ app, onRedeploy }: { app: string; onRedeploy: () => void }) {
  const qc = useQueryClient()
  const confirm = useConfirm()
  const { data } = useQuery<Volumes>({
    queryKey: queryKeys.volumes(app),
    queryFn: () => projectsService.volumes(app),
    refetchInterval: 15000,
  })
  const volumes = data?.volumes ?? []
  const editable = data?.kind === 'project'
  const pending = volumes.some((v) => !v.attached)
  const [adding, setAdding] = useState(false)
  const [backups, setBackups] = useState('')
  const [serverError, setServerError] = useState('')
  const reload = () => qc.invalidateQueries({ queryKey: queryKeys.volumes(app) })

  const form = useForm<VolumeInput>({
    resolver: zodResolver(volumeSchema),
    defaultValues: { name: 'data', path: '/data' },
  })

  const create = form.handleSubmit(async (values) => {
    setServerError('')
    try {
      await projectsService.addVolume(app, values.name, values.path)
      form.reset()
      setAdding(false)
      reload()
    } catch (err) {
      setServerError(errText(err, 'Could not add the volume.'))
    }
  })

  const del = async (name: string) => {
    if (
      !(await confirm({
        title: `Delete the ${name} volume?`,
        description: 'Its files are deleted on the next deploy. This cannot be undone.',
        confirmText: 'Delete',
        destructive: true,
      }))
    )
      return
    try {
      await projectsService.deleteVolume(app, name)
      reload()
    } catch (err) {
      setServerError(errText(err, 'Could not delete the volume.'))
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-end justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">Volumes</h2>
          <p className="text-muted-foreground mt-1 text-xs">
            {data?.kind === 'cli'
              ? "Directories that keep their files across deploys. This app's volumes come from the [[volumes]] section of its skiff.toml."
              : data?.kind === 'preview'
                ? "This preview mounts its project's volumes, on empty copies of its own — production's files are never touched."
                : 'Directories that keep their files across deploys, rollbacks and restarts — for SQLite databases, uploads and caches. Shared by every replica, worker and job; previews get empty ones of their own.'}
          </p>
        </div>
        {editable && !adding && (
          <Button size="sm" onClick={() => setAdding(true)} className="shrink-0">
            <Plus />
            Add volume
          </Button>
        )}
      </div>

      {adding && (
        <form
          onSubmit={create}
          className="flex flex-col gap-3 rounded-xl border border-foreground/10 bg-linear-to-b from-foreground/2 to-transparent p-4"
        >
          <FieldGroup className="grid gap-3 sm:grid-cols-2">
            <Field data-invalid={!!form.formState.errors.name}>
              <FieldLabel htmlFor="vol-name">Name</FieldLabel>
              <Input
                id="vol-name"
                className="font-mono"
                placeholder="data"
                aria-invalid={!!form.formState.errors.name}
                {...form.register('name')}
              />
              <FieldError errors={[form.formState.errors.name]} />
            </Field>
            <Field data-invalid={!!form.formState.errors.path}>
              <FieldLabel htmlFor="vol-path">Mount path</FieldLabel>
              <Input
                id="vol-path"
                className="font-mono"
                placeholder="/data"
                aria-invalid={!!form.formState.errors.path}
                {...form.register('path')}
              />
              <FieldError errors={[form.formState.errors.path]} />
            </Field>
          </FieldGroup>
          {serverError && <p className="text-xs text-danger">{serverError}</p>}
          <div className="flex items-center justify-end gap-3">
            <button
              type="button"
              onClick={() => {
                setAdding(false)
                setServerError('')
                form.reset()
              }}
              className="text-muted-foreground hover:text-foreground text-xs"
            >
              Cancel
            </button>
            <Button size="sm" type="submit" loading={form.formState.isSubmitting}>
              Add volume
            </Button>
          </div>
        </form>
      )}

      {volumes.length === 0 && !adding ? (
        <div className="text-muted-foreground rounded-xl border border-foreground/8 py-10 text-center text-sm">
          No volumes. Files the app writes are lost on each deploy.
        </div>
      ) : (
        volumes.length > 0 && (
          <div className="divide-y divide-foreground/5 overflow-hidden rounded-xl border border-foreground/8">
            {volumes.map((v) => (
              <div key={v.name} className="flex items-center gap-3 p-4">
                <HardDrive className="text-muted-foreground h-4 w-4 shrink-0" />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{v.name}</p>
                  <p className="text-muted-foreground mt-0.5 truncate font-mono text-xs">{v.path}</p>
                </div>
                {v.attached ? (
                  <span className="text-muted-foreground shrink-0 font-mono text-[11px] tabular-nums">
                    {fmtBytes(v.size)}
                  </span>
                ) : (
                  <span className="shrink-0 text-[11px] text-warning">Not mounted yet</span>
                )}
                {data?.kind !== 'preview' && v.attached && (
                  <button
                    onClick={() => setBackups(v.name)}
                    title="Backups"
                    className="text-muted-foreground hover:text-foreground p-1.5 transition"
                  >
                    <Archive className="h-3.5 w-3.5" />
                  </button>
                )}
                {editable && (
                  <button
                    onClick={() => del(v.name)}
                    title="Delete"
                    className="text-muted-foreground p-1.5 transition hover:text-danger"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>
            ))}
          </div>
        )
      )}
      {pending && editable && (
        <div className="flex items-center justify-between gap-3 rounded-xl border border-warning/20 bg-warning/5 px-4 py-3">
          <p className="text-xs text-warning">Redeploy to mount new volumes.</p>
          <Button size="sm" variant="outline" onClick={onRedeploy}>
            Redeploy
          </Button>
        </div>
      )}
      {serverError && !adding && <p className="text-xs text-danger">{serverError}</p>}
      <VolumeBackupsDialog
        app={app}
        volume={backups}
        open={backups !== ''}
        onOpenChange={(v) => !v && setBackups('')}
      />
    </div>
  )
}
