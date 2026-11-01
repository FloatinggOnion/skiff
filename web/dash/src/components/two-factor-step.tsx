import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { errText } from '@/lib/errors'

// The second sign-in step: a code from the authenticator app, or one of the recovery codes.
export function TwoFactorStep({
  onSubmit,
  onBack,
}: {
  onSubmit: (code: string) => Promise<void>
  onBack: () => void
}) {
  const [code, setCode] = useState('')
  const [recovery, setRecovery] = useState(false)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      await onSubmit(code.trim())
    } catch (err) {
      setError(errText(err, "That code didn't match"))
      setBusy(false)
    }
  }

  return (
    <form onSubmit={submit} className="flex flex-col gap-4">
      <div className="grid gap-2">
        <Label htmlFor="code">{recovery ? 'Recovery code' : 'Authentication code'}</Label>
        <Input
          id="code"
          autoFocus
          autoComplete="one-time-code"
          inputMode={recovery ? 'text' : 'numeric'}
          placeholder={recovery ? 'xxxxx-xxxxx' : '123456'}
          className="font-mono tracking-widest"
          value={code}
          onChange={(e) => setCode(e.target.value)}
        />
        <p className="text-muted-foreground text-xs">
          {recovery
            ? 'Each recovery code works once.'
            : 'Open your authenticator app and enter the 6-digit code for Skiff.'}
        </p>
      </div>
      {error && <p className="text-destructive text-xs">{error}</p>}
      <Button type="submit" loading={busy} disabled={!code.trim()} className="w-full">
        Verify
      </Button>
      <div className="flex items-center justify-between text-xs">
        <button
          type="button"
          onClick={onBack}
          className="text-muted-foreground hover:text-foreground"
        >
          Back
        </button>
        <button
          type="button"
          onClick={() => {
            setRecovery(!recovery)
            setCode('')
            setError('')
          }}
          className="text-muted-foreground hover:text-foreground"
        >
          {recovery ? 'Use the authenticator app' : 'Use a recovery code'}
        </button>
      </div>
    </form>
  )
}
