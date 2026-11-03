import { useState } from 'react'
import { Link } from 'react-router'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Layers, Plus, RefreshCw, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { PasswordInput } from '@/components/ui/password-input'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import { queryKeys } from '@/constants/query-keys'
import { errText } from '@/lib/errors'
import { useConfirm } from '@/providers/confirm-provider'
import {
  stacksService,
  type Stack,
  type StackPlan,
  type StackPlanItem,
  type StackRequest,
} from '@/services/api.service'
import { stackSchema, type StackInput } from '@/validations/stack'
import { StackPreviews } from './stack-previews'
import { StackUpdateDialog } from './stack-update-dialog'

function describeItem(it: StackPlanItem) {
  if (it.kind === 'database') return `Managed ${it.engine} database, with daily backups`
  if (it.kind === 'worker') return `Worker that runs alongside ${it.workerOf}`
  const parts = [it.public ? `Public on port ${it.port}` : it.port ? `Private on port ${it.port}` : 'Private, no port']
  if (it.builds) parts.push('built from source')
  if ((it.replicas ?? 1) > 1) parts.push(`${it.replicas} replicas`)
  if (it.volumes?.length) parts.push(`${it.volumes.length} volume${it.volumes.length > 1 ? 's' : ''}`)
  return parts.join(' · ')
}

function NewStackDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (v: boolean) => void }) {
  const qc = useQueryClient()
  const [plan, setPlan] = useState<StackPlan | null>(null)
  const [vars, setVars] = useState<Record<string, string>>({})
  const [askFor, setAskFor] = useState<string[]>([])
  const [error, setError] = useState('')
  const [creating, setCreating] = useState(false)

  const form = useForm<StackInput>({
    resolver: zodResolver(stackSchema),
    defaultValues: { source: 'repo', name: '', git: '', branch: '', path: '', token: '', compose: '' },
  })
  const source = form.watch('source')
  const errors = form.formState.errors

  const toRequest = (v: StackInput): StackRequest =>
    v.source === 'repo'
      ? { name: v.name, git: v.git, branch: v.branch, path: v.path, token: v.token, vars }
      : { name: v.name, compose: v.compose, vars }

  const check = form.handleSubmit(async (v) => {
    setError('')
    try {
      const p = await stacksService.plan(toRequest(v))
      setPlan(p)
      setAskFor((prev) => [...new Set([...prev, ...p.unset])])
    } catch (err) {
      setPlan(null)
      setError(errText(err, 'Could not read that Compose file.'))
    }
  })

  const reset = () => {
    form.reset()
    setPlan(null)
    setVars({})
    setAskFor([])
    setError('')
  }

  const create = form.handleSubmit(async (v) => {
    setError('')
    setCreating(true)
    try {
      await stacksService.create(toRequest(v))
      qc.invalidateQueries({ queryKey: queryKeys.stacks })
      onOpenChange(false)
      reset()
    } catch (err) {
      setError(errText(err, 'Could not create the stack. Check it again to see what changed.'))
    } finally {
      setCreating(false)
    }
  })

  return (
    <Dialog
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) reset()
      }}
    >
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>New stack from Docker Compose</DialogTitle>
        </DialogHeader>
        <form onSubmit={check} className="flex flex-col gap-4">
          <Tabs
            value={source}
            onValueChange={(v) => {
              form.setValue('source', v as StackInput['source'])
              setPlan(null)
            }}
          >
            <TabsList>
              <TabsTrigger value="repo">From a repository</TabsTrigger>
              <TabsTrigger value="paste">Paste a file</TabsTrigger>
            </TabsList>
          </Tabs>

          <FieldGroup className="gap-3">
            <Field data-invalid={!!errors.name}>
              <FieldLabel htmlFor="stack-name">Stack name</FieldLabel>
              <Input id="stack-name" className="font-mono" placeholder="shop" aria-invalid={!!errors.name} {...form.register('name')} />
              <FieldError errors={[errors.name]} />
            </Field>
            {source === 'repo' ? (
              <>
                <Field data-invalid={!!errors.git}>
                  <FieldLabel htmlFor="stack-git">Repository URL</FieldLabel>
                  <Input id="stack-git" className="font-mono" placeholder="https://github.com/you/shop" aria-invalid={!!errors.git} {...form.register('git')} />
                  <FieldError errors={[errors.git]} />
                </Field>
                <div className="grid gap-3 sm:grid-cols-2">
                  <Field>
                    <FieldLabel htmlFor="stack-branch">Branch</FieldLabel>
                    <Input id="stack-branch" className="font-mono" placeholder="Default branch" {...form.register('branch')} />
                  </Field>
                  <Field>
                    <FieldLabel htmlFor="stack-path">Compose file</FieldLabel>
                    <Input id="stack-path" className="font-mono" placeholder="Found automatically" {...form.register('path')} />
                  </Field>
                </div>
                <Field>
                  <FieldLabel htmlFor="stack-token">Access token, for a private repository</FieldLabel>
                  <PasswordInput id="stack-token" autoComplete="off" {...form.register('token')} />
                </Field>
              </>
            ) : (
              <Field data-invalid={!!errors.compose}>
                <FieldLabel htmlFor="stack-compose">Compose file</FieldLabel>
                <Textarea id="stack-compose" rows={10} spellCheck={false} className="font-mono text-xs" placeholder="services:" aria-invalid={!!errors.compose} {...form.register('compose')} />
                <FieldError errors={[errors.compose]} />
                <p className="text-muted-foreground text-xs">Services that build from source need a repository.</p>
              </Field>
            )}
          </FieldGroup>

          {askFor.length > 0 && (
            <FieldGroup className="gap-3">
              <p className="text-sm font-medium">Values this file needs</p>
              <p className="text-muted-foreground -mt-2 text-xs">Fill these in, then check the file again.</p>
              {askFor.map((k) => (
                <Field key={k}>
                  <FieldLabel htmlFor={`var-${k}`} className="font-mono text-xs">
                    {k}
                  </FieldLabel>
                  <PasswordInput
                    id={`var-${k}`}
                    autoComplete="off"
                    value={vars[k] ?? ''}
                    onChange={(e) => setVars((prev) => ({ ...prev, [k]: e.target.value }))}
                  />
                </Field>
              ))}
            </FieldGroup>
          )}

          {plan && (
            <div className="flex flex-col gap-3">
              <div className="divide-y divide-foreground/5 overflow-hidden rounded-lg border border-foreground/8">
                {plan.items.map((it) => (
                  <div key={it.service} className="p-3">
                    <p className="font-mono text-sm">{it.service}</p>
                    <p className="text-muted-foreground mt-0.5 text-xs">{describeItem(it)}</p>
                  </div>
                ))}
              </div>
              {plan.problems.length > 0 && (
                <div className="rounded-lg border border-danger/25 bg-danger/5 p-3">
                  <p className="text-sm font-medium text-danger">Fix these before creating the stack</p>
                  <ul className="mt-1.5 flex flex-col gap-1 text-xs text-danger">
                    {plan.problems.map((p) => (
                      <li key={p}>{p}</li>
                    ))}
                  </ul>
                </div>
              )}
              {plan.notes.length > 0 && (
                <div className="rounded-lg border border-foreground/8 p-3">
                  <p className="text-sm font-medium">Worth knowing</p>
                  <ul className="text-muted-foreground mt-1.5 flex flex-col gap-1 text-xs">
                    {plan.notes.map((n) => (
                      <li key={n}>{n}</li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}

          {error && <p className="text-xs text-danger">{error}</p>}

          <div className="flex items-center justify-end gap-2">
            <Button type="submit" size="sm" variant={plan ? 'outline' : 'default'} loading={form.formState.isSubmitting && !creating}>
              {plan ? 'Check again' : 'Check this file'}
            </Button>
            {plan && (
              <Button type="button" size="sm" onClick={create} loading={creating} disabled={plan.problems.length > 0}>
                Create stack
              </Button>
            )}
          </div>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function StackCard({ stack }: { stack: Stack }) {
  const qc = useQueryClient()
  const confirm = useConfirm()
  const [error, setError] = useState('')
  const [updating, setUpdating] = useState(false)

  const remove = async () => {
    if (
      !(await confirm({
        title: `Delete ${stack.name}?`,
        description:
          'This deletes every service in the stack, its volumes and its databases, including their data and backups on this server. Copies in an offsite bucket stay.',
        confirmText: 'Delete stack',
        destructive: true,
      }))
    )
      return
    setError('')
    try {
      await stacksService.remove(stack.name)
      qc.invalidateQueries({ queryKey: queryKeys.stacks })
    } catch (err) {
      setError(errText(err, 'Could not delete the stack.'))
    }
  }

  return (
    <section className="overflow-hidden rounded-xl border border-foreground/8">
      <header className="flex items-center gap-3 border-b border-foreground/5 p-4">
        <Layers className="text-muted-foreground h-4 w-4 shrink-0" />
        <h2 className="min-w-0 flex-1 truncate font-mono text-sm font-medium">{stack.name}</h2>
        <button
          onClick={() => setUpdating(true)}
          className="text-muted-foreground hover:text-foreground flex shrink-0 items-center gap-1.5 rounded-[6px] border border-foreground/12 px-2.5 py-1 text-xs transition hover:border-foreground/25"
        >
          <RefreshCw className="h-3.5 w-3.5" />
          Update
        </button>
        <button onClick={remove} title="Delete stack" className="text-muted-foreground p-1.5 transition hover:text-danger">
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </header>
      {stack.held && (
        <div className="border-b border-warning/20 bg-warning/5 px-4 py-3">
          <p className="text-sm font-medium text-warning">Changes from your latest push are waiting</p>
          <p className="text-muted-foreground mt-0.5 text-xs">{stack.held}</p>
          <p className="text-muted-foreground mt-0.5 text-xs">Use Update to review and apply them. New code still deploys.</p>
        </div>
      )}
      <ul className="divide-y divide-foreground/5">
        {stack.members.map((m) => {
          const running = m.state === 'running'
          const label = (
            <>
              <span className={'h-1.5 w-1.5 shrink-0 rounded-full ' + (running ? 'bg-success' : 'pulse-dot bg-warning')} />
              <span className="min-w-0 flex-1 truncate font-mono text-sm">{m.service}</span>
              <span className="text-muted-foreground shrink-0 text-xs">
                {m.kind === 'database' ? m.engine : m.kind === 'worker' ? 'worker' : m.public ? 'public' : 'private'}
                {!running && ` · ${m.state}`}
              </span>
            </>
          )
          const to = m.kind === 'database' ? '/databases' : `/projects/${m.app}`
          return (
            <li key={m.kind + m.service}>
              <Link to={to} className="hover:bg-foreground/3 flex items-center gap-3 px-4 py-2.5 transition">
                {label}
              </Link>
            </li>
          )
        })}
      </ul>
      {stack.fromGit && <StackPreviews stack={stack} />}
      {error && <p className="p-3 text-xs text-danger">{error}</p>}
      <StackUpdateDialog stack={stack} open={updating} onOpenChange={setUpdating} />
    </section>
  )
}

export default function StacksPage() {
  const [adding, setAdding] = useState(false)
  const { data: stacks = [], isPending, isError } = useQuery<Stack[]>({
    queryKey: queryKeys.stacks,
    queryFn: () => stacksService.list(),
    refetchInterval: 5000,
  })

  return (
    <div className="px-8 py-8">
      <header className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">Stacks</h1>
          <p className="text-muted-foreground mt-1 text-sm">
            Bring a whole backend from a Docker Compose file. Services keep finding each other by the same names.
          </p>
        </div>
        <Button size="sm" onClick={() => setAdding(true)}>
          <Plus className="h-4 w-4" />
          New stack
        </Button>
      </header>
      {isError ? (
        <p className="text-sm text-danger">Could not load your stacks.</p>
      ) : isPending ? (
        <p className="text-muted-foreground text-sm">Loading…</p>
      ) : stacks.length === 0 ? (
        <div className="text-muted-foreground rounded-xl border border-foreground/8 py-12 text-center text-sm">
          No stacks yet. Start with the Compose file you already have.
        </div>
      ) : (
        <div className="grid gap-4 lg:grid-cols-2">
          {stacks.map((s) => (
            <StackCard key={s.name} stack={s} />
          ))}
        </div>
      )}
      <NewStackDialog open={adding} onOpenChange={setAdding} />
    </div>
  )
}
