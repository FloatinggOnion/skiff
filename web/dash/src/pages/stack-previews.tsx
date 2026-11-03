import { useState } from 'react'
import { Link } from 'react-router'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQueryClient } from '@tanstack/react-query'
import { ExternalLink, GitPullRequest, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { PasswordInput } from '@/components/ui/password-input'
import { queryKeys } from '@/constants/query-keys'
import { errText } from '@/lib/errors'
import { useConfirm } from '@/providers/confirm-provider'
import { stacksService, type Stack, type StackPreviewMode } from '@/services/api.service'
import { previewPasswordSchema, type PreviewPasswordInput } from '@/validations/preview-password'
import { stackPreviewBranchSchema, type StackPreviewBranchInput } from '@/validations/stack-preview'

const options: { mode: StackPreviewMode; title: string; about: string }[] = [
  { mode: '', title: 'Off', about: 'Pull requests start nothing. You can still preview a branch by hand.' },
  { mode: 'empty', title: 'Empty databases', about: 'Each pull request gets every service, with new, empty databases.' },
  {
    mode: 'copy',
    title: 'A copy of the latest backups',
    about: 'Each pull request gets every service, with its own copy of real data.',
  },
]

function PreviewRow({ preview, onChanged }: { preview: Stack; onChanged: () => void }) {
  const confirm = useConfirm()
  const [error, setError] = useState('')
  const remove = async () => {
    if (
      !(await confirm({
        title: `Delete the preview of ${preview.branch}?`,
        description: 'This deletes its services, volumes and databases. The stack itself isn’t touched.',
        confirmText: 'Delete preview',
        destructive: true,
      }))
    )
      return
    setError('')
    try {
      await stacksService.remove(preview.name)
      onChanged()
    } catch (err) {
      setError(errText(err, 'Could not delete the preview.'))
    }
  }
  return (
    <li className="p-3">
      <div className="flex items-center gap-2">
        <GitPullRequest className="text-muted-foreground h-3.5 w-3.5 shrink-0" />
        <span className="min-w-0 flex-1 truncate font-mono text-xs">{preview.branch}</span>
        <button
          onClick={remove}
          title="Delete preview"
          aria-label={`Delete the preview of ${preview.branch}`}
          className="text-muted-foreground p-1 transition hover:text-danger"
        >
          <Trash2 className="h-3.5 w-3.5" />
        </button>
      </div>
      {preview.held && <p className="mt-1 text-xs text-warning">{preview.held}</p>}
      <ul className="mt-2 flex flex-col gap-1">
        {preview.members.map((m) => {
          const running = m.state === 'running'
          return (
            <li key={m.kind + m.service} className="flex items-center gap-2 text-xs">
              <span className={'h-1.5 w-1.5 shrink-0 rounded-full ' + (running ? 'bg-success' : 'pulse-dot bg-warning')} />
              {m.kind === 'database' ? (
                <span className="min-w-0 flex-1 truncate font-mono">{m.service}</span>
              ) : (
                <Link to={`/projects/${m.app}`} className="hover:text-foreground min-w-0 flex-1 truncate font-mono">
                  {m.service}
                </Link>
              )}
              {!running && <span className="text-muted-foreground">{m.state}</span>}
              {m.url && (
                <a href={m.url} target="_blank" rel="noreferrer" className="text-muted-foreground hover:text-foreground flex items-center gap-1">
                  Open
                  <ExternalLink className="h-3 w-3" />
                </a>
              )}
            </li>
          )
        })}
      </ul>
      {error && <p className="mt-1 text-xs text-danger">{error}</p>}
    </li>
  )
}

export function StackPreviews({ stack }: { stack: Stack }) {
  const qc = useQueryClient()
  const confirm = useConfirm()
  const [error, setError] = useState('')
  const reload = () => qc.invalidateQueries({ queryKey: queryKeys.stacks })
  const mode = stack.previewData
  const full = stack.previews.length >= stack.previewLimit

  const branchForm = useForm<StackPreviewBranchInput>({
    resolver: zodResolver(stackPreviewBranchSchema),
    defaultValues: { branch: '' },
  })
  const passwordForm = useForm<PreviewPasswordInput>({
    resolver: zodResolver(previewPasswordSchema),
    defaultValues: { password: '' },
  })

  const choose = async (next: StackPreviewMode) => {
    if (next === mode) return
    if (
      next === 'copy' &&
      !(await confirm({
        title: 'Copy real data into previews?',
        description:
          'Anyone who can open a preview can reach what its apps show. Set a preview password, and don’t use this for data that branches shouldn’t see.',
        confirmText: 'Copy data into previews',
        destructive: true,
      }))
    )
      return
    setError('')
    try {
      await stacksService.setPreviews(stack.name, next)
      reload()
    } catch (err) {
      setError(errText(err, 'Could not change this.'))
    }
  }

  const start = branchForm.handleSubmit(async ({ branch }) => {
    setError('')
    try {
      await stacksService.createPreview(stack.name, branch)
      branchForm.reset()
      reload()
    } catch (err) {
      setError(errText(err, 'Could not start the preview.'))
    }
  })

  const savePassword = passwordForm.handleSubmit(async ({ password }) => {
    setError('')
    try {
      await stacksService.setPreviews(stack.name, mode, password)
      passwordForm.reset()
      reload()
    } catch (err) {
      setError(errText(err, 'Could not set the password.'))
    }
  })

  const removePassword = async () => {
    setError('')
    try {
      await stacksService.setPreviews(stack.name, mode, '')
      reload()
    } catch (err) {
      setError(errText(err, 'Could not remove the password.'))
    }
  }

  return (
    <div className="flex flex-col gap-3 border-t border-foreground/5 p-4">
      <div>
        <h3 className="text-sm font-medium">Previews for pull requests</h3>
        <p className="text-muted-foreground mt-0.5 text-xs">
          Each pull request gets its own copy of the stack, removed when it closes. Up to {stack.previewLimit} run at once.
        </p>
      </div>

      {stack.previewError && (
        <p className="rounded-lg border border-warning/20 bg-warning/5 px-3 py-2 text-xs text-warning">{stack.previewError}</p>
      )}

      <div className="flex flex-col gap-1.5" role="radiogroup" aria-label={`Previews for ${stack.name}`}>
        {options.map((o) => (
          <label
            key={o.mode || 'off'}
            className="hover:bg-foreground/3 flex cursor-pointer items-start gap-2.5 rounded-lg border border-foreground/8 p-2.5 transition"
          >
            <input type="radio" name={`previews-${stack.name}`} className="mt-0.5" checked={mode === o.mode} onChange={() => void choose(o.mode)} />
            <span>
              <span className="block text-sm">{o.title}</span>
              <span className="text-muted-foreground block text-xs">{o.about}</span>
            </span>
          </label>
        ))}
      </div>

      <form onSubmit={savePassword} className="flex flex-col gap-1.5">
        <Field data-invalid={!!passwordForm.formState.errors.password}>
          <FieldLabel htmlFor={`preview-password-${stack.name}`} className="text-xs">
            {stack.previewPassword ? 'Previews ask for a password. Change it' : 'Ask visitors for a password'}
          </FieldLabel>
          <div className="flex items-center gap-2">
            <PasswordInput
              id={`preview-password-${stack.name}`}
              autoComplete="new-password"
              aria-invalid={!!passwordForm.formState.errors.password}
              {...passwordForm.register('password')}
            />
            <Button size="sm" variant="outline" type="submit" loading={passwordForm.formState.isSubmitting}>
              Save
            </Button>
            {stack.previewPassword && (
              <Button size="sm" variant="outline" type="button" onClick={removePassword}>
                Remove
              </Button>
            )}
          </div>
          <FieldError errors={[passwordForm.formState.errors.password]} />
        </Field>
      </form>

      <form onSubmit={start}>
        <Field data-invalid={!!branchForm.formState.errors.branch}>
          <FieldLabel htmlFor={`preview-branch-${stack.name}`} className="text-xs">
            Preview a branch now
          </FieldLabel>
          <div className="flex items-center gap-2">
            <Input
              id={`preview-branch-${stack.name}`}
              className="font-mono"
              placeholder="feat/login"
              disabled={full}
              aria-invalid={!!branchForm.formState.errors.branch}
              {...branchForm.register('branch')}
            />
            <Button size="sm" variant="outline" type="submit" disabled={full} loading={branchForm.formState.isSubmitting}>
              Start
            </Button>
          </div>
          <FieldError errors={[branchForm.formState.errors.branch]} />
        </Field>
      </form>

      {stack.previews.length > 0 && (
        <ul className="divide-y divide-foreground/5 rounded-lg border border-foreground/8">
          {stack.previews.map((p) => (
            <PreviewRow key={p.name} preview={p} onChanged={reload} />
          ))}
        </ul>
      )}
      {error && <p className="text-xs text-danger">{error}</p>}
    </div>
  )
}
