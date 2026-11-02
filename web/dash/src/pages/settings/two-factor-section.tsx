import { useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import QRCode from 'react-qr-code'
import { Download, ShieldCheck } from 'lucide-react'
import { authService, type TwoFactorStatus } from '@/services/api.service'
import { queryKeys } from '@/constants/query-keys'
import { errText } from '@/lib/errors'
import { useCopy } from '@/hooks/use-copy'
import { Button } from '@/components/ui/button'
import { Copyable } from '@/components/copyable'
import { Section, RevealInput, inputCls } from './ui'

type Step =
  | { kind: 'idle' }
  | { kind: 'password'; action: 'setup' | 'disable' | 'recovery' }
  | { kind: 'scan'; secret: string; uri: string }
  | { kind: 'codes'; codes: string[] }

export function TwoFactorSection() {
  const qc = useQueryClient()
  const { data: status } = useQuery<TwoFactorStatus>({
    queryKey: queryKeys.twoFactor,
    queryFn: () => authService.twoFactor(),
  })
  const [step, setStep] = useState<Step>({ kind: 'idle' })
  const [password, setPassword] = useState('')
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const reset = (next: Step = { kind: 'idle' }) => {
    setStep(next)
    setPassword('')
    setCode('')
    setError('')
    setBusy(false)
  }
  const run = async (fn: () => Promise<void>, fallback: string) => {
    setBusy(true)
    setError('')
    try {
      await fn()
    } catch (err) {
      setError(errText(err, fallback))
      setBusy(false)
    }
  }
  const reload = () => qc.invalidateQueries({ queryKey: queryKeys.twoFactor })

  const submitPassword = (e: FormEvent) => {
    e.preventDefault()
    if (step.kind !== 'password') return
    if (step.action === 'setup') {
      return run(async () => {
        const { secret, uri } = await authService.twoFactorSetup(password)
        reset({ kind: 'scan', secret, uri })
      }, 'Could not start setup.')
    }
    if (step.action === 'disable') {
      return run(async () => {
        await authService.twoFactorDisable(password, code.trim())
        reset()
        reload()
      }, 'Could not turn two-factor off.')
    }
    return run(async () => {
      const { recoveryCodes } = await authService.twoFactorRecovery(password, code.trim())
      reset({ kind: 'codes', codes: recoveryCodes })
      reload()
    }, 'Could not make new codes.')
  }

  const submitCode = (e: FormEvent) => {
    e.preventDefault()
    run(async () => {
      const { recoveryCodes } = await authService.twoFactorEnable(code.trim())
      reset({ kind: 'codes', codes: recoveryCodes })
      reload()
    }, "That code didn't match.")
  }

  const enabled = status?.enabled ?? false
  const errorLine = error && <p className="text-xs text-danger">{error}</p>
  const cancel = (
    <button
      type="button"
      onClick={() => reset()}
      className="text-muted-foreground hover:text-foreground text-xs"
    >
      Cancel
    </button>
  )

  return (
    <Section
      title="Two-factor authentication"
      description="Ask for a code from an authenticator app, like 1Password or Google Authenticator, after your password."
    >
      {step.kind === 'idle' && (
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-2 text-sm">
            {enabled ? (
              <>
                <ShieldCheck className="h-4 w-4 text-success" />
                <span>On</span>
                <span className="text-muted-foreground text-xs">
                  · {status?.recoveryLeft ?? 0} recovery codes left
                </span>
              </>
            ) : (
              <span className="text-muted-foreground">Off</span>
            )}
          </div>
          {enabled ? (
            <div className="flex gap-2">
              <Button
                size="sm"
                variant="outline"
                onClick={() => reset({ kind: 'password', action: 'recovery' })}
              >
                New recovery codes
              </Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => reset({ kind: 'password', action: 'disable' })}
              >
                Turn off
              </Button>
            </div>
          ) : (
            <Button size="sm" onClick={() => reset({ kind: 'password', action: 'setup' })}>
              Set up
            </Button>
          )}
        </div>
      )}

      {step.kind === 'password' && (
        <form onSubmit={submitPassword} className="space-y-3">
          <RevealInput
            placeholder="Current password"
            autoComplete="current-password"
            autoFocus
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
          {step.action !== 'setup' && (
            <input
              className={inputCls + ' font-mono'}
              placeholder="Code from your app, or a recovery code"
              autoComplete="one-time-code"
              value={code}
              onChange={(e) => setCode(e.target.value)}
            />
          )}
          {errorLine}
          <div className="flex items-center justify-end gap-3">
            {cancel}
            <Button
              type="submit"
              size="sm"
              loading={busy}
              disabled={!password || (step.action !== 'setup' && !code.trim())}
              variant={step.action === 'disable' ? 'destructive' : 'default'}
            >
              {step.action === 'setup'
                ? 'Continue'
                : step.action === 'disable'
                  ? 'Turn off two-factor'
                  : 'Make new codes'}
            </Button>
          </div>
        </form>
      )}

      {step.kind === 'scan' && (
        <form onSubmit={submitCode} className="space-y-4">
          <div className="flex flex-col gap-4 sm:flex-row sm:items-center">
            <div className="w-fit shrink-0 rounded-lg bg-foreground p-3">
              <QRCode value={step.uri} size={148} />
            </div>
            <div className="min-w-0 space-y-2 text-xs">
              <p className="text-muted-foreground">
                Scan this with your authenticator app, or enter the key by hand:
              </p>
              <div className="rounded-md border border-foreground/10 bg-inset px-2.5 py-1.5 font-mono">
                <Copyable text={step.secret} />
              </div>
              <p className="text-muted-foreground">Then type the 6-digit code it shows.</p>
            </div>
          </div>
          <input
            className={inputCls + ' font-mono tracking-widest'}
            placeholder="123456"
            inputMode="numeric"
            autoComplete="one-time-code"
            autoFocus
            value={code}
            onChange={(e) => setCode(e.target.value)}
          />
          {errorLine}
          <div className="flex items-center justify-end gap-3">
            {cancel}
            <Button type="submit" size="sm" loading={busy} disabled={code.trim().length < 6}>
              Turn on
            </Button>
          </div>
        </form>
      )}

      {step.kind === 'codes' && <RecoveryCodes codes={step.codes} onDone={() => reset()} />}
    </Section>
  )
}

function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const { copied, copy } = useCopy()
  const text = codes.join('\n') + '\n'
  const download = () => {
    const url = URL.createObjectURL(new Blob([text], { type: 'text/plain' }))
    const a = document.createElement('a')
    a.href = url
    a.download = 'skiff-recovery-codes.txt'
    a.click()
    URL.revokeObjectURL(url)
  }
  return (
    <div className="space-y-3">
      <p className="text-xs text-warning">
        Save these recovery codes somewhere safe. Each one signs you in once if you lose your
        phone, and they won&apos;t be shown again.
      </p>
      <div className="grid grid-cols-2 gap-x-6 gap-y-1 rounded-md border border-foreground/10 bg-inset p-3 font-mono text-sm">
        {codes.map((c) => (
          <span key={c}>{c}</span>
        ))}
      </div>
      <div className="flex items-center justify-end gap-2">
        <Button size="sm" variant="outline" onClick={() => copy(text)}>
          {copied ? 'Copied' : 'Copy'}
        </Button>
        <Button size="sm" variant="outline" onClick={download}>
          <Download />
          Download
        </Button>
        <Button size="sm" onClick={onDone}>
          I&apos;ve saved them
        </Button>
      </div>
    </div>
  )
}
