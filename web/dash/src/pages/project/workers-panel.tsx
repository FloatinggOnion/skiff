import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { Link } from 'react-router'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Cog, Plus, ScrollText, Terminal, Trash2 } from 'lucide-react'
import { ConsoleDrawer } from '@/components/console-drawer'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { queryKeys } from '@/constants/query-keys'
import { errText } from '@/lib/errors'
import { useConfirm } from '@/providers/confirm-provider'
import { projectsService, type Worker } from '@/services/api.service'
import { workerSchema, type WorkerInput } from '@/validations/worker'
import { Stepper } from './stepper'

export function WorkersPanel({ app }: { app: string }) {
  const qc = useQueryClient()
  const confirm = useConfirm()
  const { data: workers = [] } = useQuery<Worker[]>({
    queryKey: queryKeys.workers(app),
    queryFn: () => projectsService.workers(app),
    refetchInterval: 8000,
  })
  const [adding, setAdding] = useState(false)
  const [consoleFor, setConsoleFor] = useState<string | null>(null)
  const [serverError, setServerError] = useState('')
  const reload = () => qc.invalidateQueries({ queryKey: queryKeys.workers(app) })

  const form = useForm<WorkerInput>({
    resolver: zodResolver(workerSchema),
    defaultValues: { name: 'worker', command: '', replicas: 1 },
  })
  const replicas = form.watch('replicas')
  const setReplicas = (n: number) =>
    form.setValue('replicas', Math.min(10, Math.max(1, n)), { shouldDirty: true })

  const create = form.handleSubmit(async (values) => {
    setServerError('')
    try {
      await projectsService.setWorker(app, values.name, values.command, values.replicas)
      form.reset()
      setAdding(false)
      reload()
    } catch (err) {
      setServerError(errText(err, 'Could not save the worker.'))
    }
  })

  const del = async (id: string) => {
    if (
      !(await confirm({ title: 'Delete this worker?', confirmText: 'Delete', destructive: true }))
    )
      return
    await projectsService.deleteWorker(id)
    reload()
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-end justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">Background workers</h2>
          <p className="text-muted-foreground mt-1 text-xs">
            Long-running processes from this app's image with a different command and no HTTP port —
            recreated on every deploy. For scheduled one-offs, use Jobs.
          </p>
        </div>
        {!adding && (
          <Button size="sm" onClick={() => setAdding(true)} className="shrink-0">
            <Plus />
            New worker
          </Button>
        )}
      </div>

      {adding && (
        <form
          onSubmit={create}
          className="flex flex-col gap-3 rounded-xl border border-foreground/10 bg-linear-to-b from-foreground/2 to-transparent p-4"
        >
          <FieldGroup className="gap-3">
            <div className="grid gap-3 sm:grid-cols-[1fr_auto]">
              <Field data-invalid={!!form.formState.errors.name}>
                <FieldLabel htmlFor="wk-name">Name</FieldLabel>
                <Input
                  id="wk-name"
                  className="font-mono"
                  placeholder="worker"
                  aria-invalid={!!form.formState.errors.name}
                  {...form.register('name')}
                />
                <FieldError errors={[form.formState.errors.name]} />
              </Field>
              <Field>
                <FieldLabel>Replicas</FieldLabel>
                <Stepper
                  value={replicas}
                  onDec={() => setReplicas(replicas - 1)}
                  onInc={() => setReplicas(replicas + 1)}
                />
              </Field>
            </div>
            <Field data-invalid={!!form.formState.errors.command}>
              <FieldLabel htmlFor="wk-command">Command</FieldLabel>
              <Input
                id="wk-command"
                className="font-mono"
                placeholder="node worker.js"
                aria-invalid={!!form.formState.errors.command}
                {...form.register('command')}
              />
              <FieldError errors={[form.formState.errors.command]} />
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
              Add worker
            </Button>
          </div>
        </form>
      )}

      {workers.length === 0 && !adding ? (
        <div className="text-muted-foreground rounded-xl border border-foreground/8 py-14 text-center text-sm">
          No workers yet.
        </div>
      ) : (
        workers.length > 0 && (
          <div className="divide-y divide-foreground/5 overflow-hidden rounded-xl border border-foreground/8">
            {workers.map((wk) => (
              <div key={wk.id} className="flex items-center gap-3 p-4">
                <Cog className="text-muted-foreground h-4 w-4 shrink-0" />
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm font-medium">{wk.name}</p>
                  <p className="text-muted-foreground mt-0.5 truncate font-mono text-xs">
                    {wk.command}
                  </p>
                </div>
                <span
                  className={
                    'shrink-0 font-mono text-[11px] tabular-nums ' +
                    (wk.running >= wk.replicas ? 'text-success' : 'text-warning')
                  }
                >
                  {wk.running}/{wk.replicas} running
                </span>
                <Link
                  to={`/logs?app=${encodeURIComponent(app)}`}
                  title="Logs"
                  aria-label={`Logs for ${wk.name}`}
                  className="text-muted-foreground hover:text-foreground p-1.5 transition"
                >
                  <ScrollText className="h-3.5 w-3.5" />
                </Link>
                <button
                  onClick={() => setConsoleFor(wk.name)}
                  disabled={wk.running === 0}
                  title={wk.running === 0 ? "This worker isn't running" : 'Open a shell inside this worker'}
                  aria-label={`Console for ${wk.name}`}
                  className="text-muted-foreground hover:text-foreground p-1.5 transition disabled:pointer-events-none disabled:opacity-40"
                >
                  <Terminal className="h-3.5 w-3.5" />
                </button>
                <button
                  onClick={() => del(wk.id)}
                  title="Delete"
                  className="text-muted-foreground p-1.5 transition hover:text-danger"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              </div>
            ))}
          </div>
        )
      )}
      {serverError && !adding && <p className="text-xs text-danger">{serverError}</p>}
      {consoleFor && (
        <ConsoleDrawer app={app} worker={consoleFor} onClose={() => setConsoleFor(null)} />
      )}
    </div>
  )
}
