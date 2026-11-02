import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Play, Plus, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { queryKeys } from '@/constants/query-keys'
import { relTime } from '@/lib/format'
import { errText } from '@/lib/errors'
import { useConfirm } from '@/providers/confirm-provider'
import { projectsService, type Job } from '@/services/api.service'
import { jobSchema, type JobInput } from '@/validations/job'

export function JobsPanel({ app }: { app: string }) {
  const qc = useQueryClient()
  const confirm = useConfirm()
  const { data: jobs = [] } = useQuery<Job[]>({
    queryKey: queryKeys.jobs(app),
    queryFn: () => projectsService.jobs(app),
  })
  const [adding, setAdding] = useState(false)
  const [busy, setBusy] = useState('')
  const [serverError, setServerError] = useState('')
  const [output, setOutput] = useState<{ id: string; ok: boolean; text: string } | null>(null)
  const reload = () => qc.invalidateQueries({ queryKey: queryKeys.jobs(app) })

  const form = useForm<JobInput>({
    resolver: zodResolver(jobSchema),
    defaultValues: { name: '', schedule: '0 3 * * *', command: '' },
  })

  const create = form.handleSubmit(async (values) => {
    setServerError('')
    try {
      await projectsService.createJob(app, values.name || 'job', values.schedule, values.command)
      form.reset()
      setAdding(false)
      reload()
    } catch (err) {
      setServerError(errText(err, 'Could not create the job.'))
    }
  })

  const run = async (id: string) => {
    setBusy(id)
    setOutput(null)
    setServerError('')
    try {
      const r = await projectsService.runJob(id)
      setOutput({ id, ok: r.ok, text: r.output?.trim() || (r.ok ? 'Done.' : 'Failed.') })
      reload()
    } catch (err) {
      setServerError(errText(err, 'Could not run the job.'))
    } finally {
      setBusy('')
    }
  }

  const del = async (id: string) => {
    if (!(await confirm({ title: 'Delete this job?', confirmText: 'Delete', destructive: true })))
      return
    await projectsService.deleteJob(id)
    reload()
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-end justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">Scheduled jobs</h2>
          <p className="text-muted-foreground mt-1 text-xs">
            Run a command on a cron schedule in a one-off container, with this app's environment.
          </p>
        </div>
        {!adding && (
          <Button size="sm" onClick={() => setAdding(true)} className="shrink-0">
            <Plus />
            New job
          </Button>
        )}
      </div>

      {adding && (
        <form
          onSubmit={create}
          className="flex flex-col gap-3 rounded-xl border border-foreground/10 bg-linear-to-b from-foreground/2 to-transparent p-4"
        >
          <FieldGroup className="gap-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="job-name">Name</FieldLabel>
                <Input id="job-name" placeholder="nightly cleanup" {...form.register('name')} />
              </Field>
              <Field data-invalid={!!form.formState.errors.schedule}>
                <FieldLabel htmlFor="job-schedule">Schedule (cron)</FieldLabel>
                <Input
                  id="job-schedule"
                  className="font-mono"
                  placeholder="0 3 * * *"
                  aria-invalid={!!form.formState.errors.schedule}
                  {...form.register('schedule')}
                />
                <FieldError errors={[form.formState.errors.schedule]} />
              </Field>
            </div>
            <Field data-invalid={!!form.formState.errors.command}>
              <FieldLabel htmlFor="job-command">Command</FieldLabel>
              <Input
                id="job-command"
                className="font-mono"
                placeholder="npm run cleanup"
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
              Add job
            </Button>
          </div>
        </form>
      )}

      {jobs.length === 0 && !adding ? (
        <div className="text-muted-foreground rounded-xl border border-foreground/8 py-14 text-center text-sm">
          No scheduled jobs yet.
        </div>
      ) : (
        jobs.length > 0 && (
          <div className="divide-y divide-foreground/5 overflow-hidden rounded-xl border border-foreground/8">
            {jobs.map((j) => (
              <div key={j.id} className="p-4">
                <div className="flex items-center gap-3">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">{j.name}</p>
                    <p className="text-muted-foreground mt-0.5 truncate font-mono text-xs">
                      {j.command}
                    </p>
                  </div>
                  <span className="text-muted-foreground shrink-0 font-mono text-[11px]">
                    {j.schedule}
                  </span>
                  <button
                    onClick={() => run(j.id)}
                    disabled={busy === j.id}
                    title="Run now"
                    className="text-muted-foreground hover:text-foreground p-1.5 disabled:opacity-40"
                  >
                    <Play className="h-3.5 w-3.5" />
                  </button>
                  <button
                    onClick={() => del(j.id)}
                    title="Delete"
                    className="text-muted-foreground p-1.5 transition hover:text-danger"
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </button>
                </div>
                <div className="text-muted-foreground mt-2 flex flex-wrap gap-x-4 gap-y-1 text-[11px]">
                  <span>next {fmtWhen(j.next)}</span>
                  <span>
                    last run{' '}
                    {j.lastRun ? (
                      <>
                        {relTime(j.lastRun)} ·{' '}
                        <span className={j.lastOk ? 'text-success' : 'text-danger'}>
                          {j.lastOk ? 'ok' : 'failed'}
                        </span>
                      </>
                    ) : (
                      'never'
                    )}
                  </span>
                </div>
                {output?.id === j.id && (
                  <pre
                    className={
                      'mt-2 max-h-40 overflow-auto rounded-md border p-2.5 font-mono text-[11px] whitespace-pre-wrap ' +
                      (output.ok
                        ? 'border-foreground/8 bg-inset'
                        : 'border-danger/25 bg-danger/5 text-danger')
                    }
                  >
                    {output.text}
                  </pre>
                )}
              </div>
            ))}
          </div>
        )
      )}
      {serverError && !adding && <p className="text-xs text-danger">{serverError}</p>}
    </div>
  )
}

function fmtWhen(unix: number): string {
  if (!unix) return '—'
  return new Date(unix * 1000).toLocaleString([], {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  })
}
