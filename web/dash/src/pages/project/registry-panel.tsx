import { useState } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { KeyRound } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Field, FieldError, FieldGroup, FieldLabel } from '@/components/ui/field'
import { queryKeys } from '@/constants/query-keys'
import { errText } from '@/lib/errors'
import { useConfirm } from '@/providers/confirm-provider'
import { projectsService, type RegistryCreds } from '@/services/api.service'
import { registrySchema, type RegistryInput } from '@/validations/image'

export function RegistryPanel({ app }: { app: string }) {
  const qc = useQueryClient()
  const confirm = useConfirm()
  const { data } = useQuery<RegistryCreds>({
    queryKey: queryKeys.registry(app),
    queryFn: () => projectsService.registry(app),
  })
  const [editing, setEditing] = useState(false)
  const [serverError, setServerError] = useState('')
  const reload = () => qc.invalidateQueries({ queryKey: queryKeys.registry(app) })

  const form = useForm<RegistryInput>({
    resolver: zodResolver(registrySchema),
    defaultValues: { username: '', password: '' },
  })
  const errors = form.formState.errors

  const save = form.handleSubmit(async (values) => {
    setServerError('')
    try {
      await projectsService.setRegistry(app, values.username, values.password)
      form.reset()
      setEditing(false)
      reload()
    } catch (err) {
      setServerError(errText(err, 'Could not save the credentials.'))
    }
  })

  const remove = async () => {
    if (
      !(await confirm({
        title: 'Remove the registry credentials?',
        description: 'Deploys pull the image without signing in, which works only if it is public.',
        confirmText: 'Remove',
        destructive: true,
      }))
    )
      return
    try {
      await projectsService.removeRegistry(app)
      reload()
    } catch (err) {
      setServerError(errText(err, 'Could not remove the credentials.'))
    }
  }

  const startEdit = () => {
    form.reset({ username: data?.username ?? '', password: '' })
    setEditing(true)
  }

  return (
    <div className="mt-10 flex flex-col gap-4">
      <div className="flex items-end justify-between gap-3">
        <div>
          <h2 className="text-sm font-semibold">Registry credentials</h2>
          <p className="text-muted-foreground mt-1 text-xs">
            For a private image, the username and a password or access token for{' '}
            <span className="font-mono">{data?.host ?? 'its registry'}</span>. Public images need
            none.
          </p>
        </div>
        {!editing && (
          <Button size="sm" variant="outline" onClick={startEdit} className="shrink-0">
            {data?.set ? 'Change' : 'Add credentials'}
          </Button>
        )}
      </div>

      {editing ? (
        <form
          onSubmit={save}
          className="flex flex-col gap-3 rounded-xl border border-foreground/10 bg-linear-to-b from-foreground/2 to-transparent p-4"
        >
          <FieldGroup className="grid gap-3 sm:grid-cols-2">
            <Field data-invalid={!!errors.username}>
              <FieldLabel htmlFor="reg-user">Username</FieldLabel>
              <Input
                id="reg-user"
                autoComplete="off"
                aria-invalid={!!errors.username}
                {...form.register('username')}
              />
              <FieldError errors={[errors.username]} />
            </Field>
            <Field data-invalid={!!errors.password}>
              <FieldLabel htmlFor="reg-pass">Password or token</FieldLabel>
              <Input
                id="reg-pass"
                type="password"
                autoComplete="new-password"
                aria-invalid={!!errors.password}
                {...form.register('password')}
              />
              <FieldError errors={[errors.password]} />
            </Field>
          </FieldGroup>
          {serverError && <p className="text-xs text-danger">{serverError}</p>}
          <div className="flex items-center justify-end gap-3">
            <button
              type="button"
              onClick={() => {
                setEditing(false)
                setServerError('')
              }}
              className="text-muted-foreground hover:text-foreground text-xs"
            >
              Cancel
            </button>
            <Button size="sm" type="submit" loading={form.formState.isSubmitting}>
              Save
            </Button>
          </div>
        </form>
      ) : data?.set ? (
        <div className="flex items-center gap-3 rounded-xl border border-foreground/8 p-4">
          <KeyRound className="text-muted-foreground h-4 w-4 shrink-0" />
          <p className="min-w-0 flex-1 truncate font-mono text-sm">{data.username}</p>
          <button
            onClick={remove}
            className="text-muted-foreground text-xs transition hover:text-danger"
          >
            Remove
          </button>
        </div>
      ) : null}
      {serverError && !editing && <p className="text-xs text-danger">{serverError}</p>}
    </div>
  )
}
