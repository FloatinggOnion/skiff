import { useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Check } from 'lucide-react'
import { authService } from '@/services/api.service'
import { errText } from '@/lib/errors'
import { Button } from '@/components/ui/button'
import { Section, RevealInput } from './ui'

export function PasswordSection() {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const save = useMutation({
    mutationFn: () => authService.changePassword(current, next),
    onSuccess: () => {
      setCurrent('')
      setNext('')
    },
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    save.mutate()
  }

  return (
    <Section title="Password" description="Use at least 8 characters.">
      <form onSubmit={submit} className="space-y-3">
        <RevealInput
          placeholder="Current password"
          autoComplete="current-password"
          value={current}
          onChange={(e) => {
            setCurrent(e.target.value)
            save.reset()
          }}
        />
        <RevealInput
          placeholder="New password"
          autoComplete="new-password"
          value={next}
          onChange={(e) => {
            setNext(e.target.value)
            save.reset()
          }}
        />
        {save.isError && (
          <p className="text-xs text-danger">
            {errText(save.error, 'Could not change your password.')}
          </p>
        )}
        <div className="flex justify-end">
          <Button
            type="submit"
            size="sm"
            loading={save.isPending}
            disabled={!current || next.length < 8}
          >
            {save.isSuccess ? (
              <>
                <Check className="h-4 w-4" />
                Updated
              </>
            ) : (
              'Update password'
            )}
          </Button>
        </div>
      </form>
    </Section>
  )
}
