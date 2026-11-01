import { useState, type FormEvent } from 'react'
import { authService } from '@/services/api.service'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { PasswordInput } from '@/components/ui/password-input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { LogoMark } from '@/components/logo'
import { errText } from '@/lib/errors'
import { TwoFactorStep } from '@/components/two-factor-step'

export default function AcceptInvite({ token }: { token: string }) {
  const [name, setName] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [ticket, setTicket] = useState('')

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const step = await authService.accept(token, name.trim(), password)
      if (step && step.twoFactor) {
        setTicket(step.ticket)
        setBusy(false)
        return
      }
      window.location.href = '/'
    } catch (err) {
      setError(errText(err, 'Could not accept the invite'))
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <LogoMark className="mb-1 h-7 w-7" />
          <CardTitle className="text-lg tracking-tight">Join the team on Skiff</CardTitle>
          <CardDescription>
            Set a password to accept your invite — or use your existing password if you already have
            an account.
          </CardDescription>
        </CardHeader>
        <CardContent>
          {ticket ? (
            <TwoFactorStep
              onSubmit={async (code) => {
                await authService.loginCode(ticket, code)
                window.location.href = '/'
              }}
              onBack={() => window.location.reload()}
            />
          ) : (
            <form onSubmit={submit} className="flex flex-col gap-4">
              <div className="grid gap-2">
                <Label htmlFor="name">Name</Label>
                <Input id="name" autoFocus value={name} onChange={(e) => setName(e.target.value)} />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="password">Password</Label>
                <PasswordInput
                  id="password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </div>
              {error && <p className="text-destructive text-xs">{error}</p>}
              <Button type="submit" loading={busy} className="w-full">
                Accept invite
              </Button>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
