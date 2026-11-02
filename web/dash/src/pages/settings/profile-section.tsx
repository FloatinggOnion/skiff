import { useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { authService, type User } from '@/services/api.service'
import { errText } from '@/lib/errors'
import { Section, Field, SaveButton, inputCls } from './ui'

export function ProfileSection({ user, onSaved }: { user: User; onSaved: () => Promise<unknown> }) {
  const [name, setName] = useState(user.name)
  const dirty = name.trim() !== '' && name.trim() !== user.name
  const save = useMutation({
    mutationFn: () => authService.updateProfile(name.trim()),
    onSuccess: () => onSaved(),
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate()
  }

  return (
    <Section title="Profile" description="Your name as it appears across the dashboard.">
      <form onSubmit={submit} className="space-y-3">
        <Field label="Name">
          <input
            className={inputCls}
            value={name}
            onChange={(e) => {
              setName(e.target.value)
              save.reset()
            }}
          />
        </Field>
        <Field label="Email">
          <input className={inputCls} value={user.email} disabled readOnly />
        </Field>
        {save.isError && (
          <p className="text-xs text-danger">
            {errText(save.error, 'Could not save your profile.')}
          </p>
        )}
        <div className="flex justify-end">
          <SaveButton busy={save.isPending} saved={save.isSuccess} disabled={!dirty} />
        </div>
      </form>
    </Section>
  )
}
