import { useState, type FormEvent } from 'react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { PasswordInput } from '@/components/ui/password-input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { LogoMark } from '@/components/logo'
import { errText } from '@/lib/errors'
import { TwoFactorStep } from '@/components/two-factor-step'

export default function Login({
  onLogin,
  onCode,
}: {
  onLogin: (email: string, password: string) => Promise<string | null>
  onCode: (ticket: string, code: string) => Promise<void>
}) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [ticket, setTicket] = useState('')

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      const next = await onLogin(email.trim(), password)
      if (next) {
        setTicket(next)
        setBusy(false)
      }
    } catch (err) {
      setError(errText(err, 'Wrong email or password'))
      setBusy(false)
    }
  }

  return (
    <div className="flex min-h-screen items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <LogoMark className="mb-1 h-7 w-7" />
          <CardTitle className="text-lg tracking-tight">Sign in to Skiff</CardTitle>
          <CardDescription>
            {ticket ? 'One more step to confirm it’s you.' : 'Your self-hosted deploy console.'}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {ticket ? (
            <TwoFactorStep
              onSubmit={(code) => onCode(ticket, code)}
              onBack={() => {
                setTicket('')
                setPassword('')
              }}
            />
          ) : (
            <form onSubmit={submit} className="flex flex-col gap-4">
              <div className="grid gap-2">
                <Label htmlFor="email">Email</Label>
                <Input
                  id="email"
                  type="email"
                  autoFocus
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="password">Password</Label>
                <PasswordInput
                  id="password"
                  autoComplete="current-password"
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                />
              </div>
              {error && <p className="text-destructive text-xs">{error}</p>}
              <Button type="submit" loading={busy} className="w-full">
                Sign in
              </Button>
            </form>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
