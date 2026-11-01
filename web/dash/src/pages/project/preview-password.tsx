import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { Lock, LockOpen } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Field, FieldError, FieldLabel } from '@/components/ui/field'
import { useConfirm } from '@/providers/confirm-provider'
import { errText } from '@/lib/errors'
import { projectsService } from '@/services/api.service'
import { previewPasswordSchema, type PreviewPasswordInput } from '@/validations/preview-password'

export function PreviewPassword({
  project,
  enabled,
  onChanged,
}: {
  project: string
  enabled: boolean
  onChanged: () => void
}) {
  const confirm = useConfirm()
  const [editing, setEditing] = useState(false)
  const [error, setError] = useState('')
  const form = useForm<PreviewPasswordInput>({
    resolver: zodResolver(previewPasswordSchema),
    defaultValues: { password: '' },
  })
  const { register, handleSubmit, reset, formState } = form

  const save = handleSubmit(async ({ password }) => {
    setError('')
    try {
      await projectsService.setPreviewPassword(project, password)
      reset()
      setEditing(false)
      onChanged()
    } catch (e) {
      setError(errText(e))
    }
  })

  const remove = async () => {
    if (
      !(await confirm({
        title: 'Remove the preview password?',
        description: 'Anyone with a preview URL will be able to open it.',
        confirmText: 'Remove',
        destructive: true,
      }))
    )
      return
    setError('')
    try {
      await projectsService.removePreviewPassword(project)
      onChanged()
    } catch (e) {
      setError(errText(e))
    }
  }

  const showForm = !enabled || editing

  return (
    <section className="rounded-xl border border-white/8 p-4">
      <div className="flex items-start justify-between gap-4">
        <div>
          <h2 className="flex items-center gap-2 text-sm font-medium">
            {enabled ? (
              <Lock className="h-3.5 w-3.5 text-emerald-300" />
            ) : (
              <LockOpen className="text-muted-foreground h-3.5 w-3.5" />
            )}
            Password protection
          </h2>
          <p className="text-muted-foreground mt-1 text-xs">
            {enabled
              ? 'Visitors enter a password before they can open any preview of this project. Your production URL stays public.'
              : 'Ask visitors for a password before they can open a preview. Your production URL stays public.'}
          </p>
        </div>
        {enabled && !editing && (
          <div className="flex shrink-0 gap-2">
            <Button size="sm" variant="outline" onClick={() => setEditing(true)}>
              Change
            </Button>
            <Button size="sm" variant="outline" onClick={remove}>
              Remove
            </Button>
          </div>
        )}
      </div>

      {showForm && (
        <form onSubmit={save} className="mt-3 flex flex-col gap-2 sm:flex-row sm:items-start">
          <Field data-invalid={!!formState.errors.password} className="min-w-0 flex-1">
            <FieldLabel htmlFor="preview-password" className="sr-only">
              Preview password
            </FieldLabel>
            <Input
              id="preview-password"
              type="password"
              autoComplete="new-password"
              placeholder={enabled ? 'New password' : 'Password'}
              aria-invalid={!!formState.errors.password}
              {...register('password')}
            />
            <FieldError errors={[formState.errors.password]} />
          </Field>
          <div className="flex shrink-0 gap-2">
            {editing && (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                onClick={() => {
                  reset()
                  setEditing(false)
                }}
              >
                Cancel
              </Button>
            )}
            <Button type="submit" size="sm" disabled={formState.isSubmitting}>
              {enabled ? 'Save password' : 'Protect previews'}
            </Button>
          </div>
        </form>
      )}
      {error && <p className="mt-2 text-xs text-rose-300">{error}</p>}
    </section>
  )
}
