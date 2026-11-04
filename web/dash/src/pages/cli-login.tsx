import { useState, type FormEvent } from 'react'
import { Terminal } from 'lucide-react'
import { useAuthContext } from '@/context/auth-context'
import { useCliLogin } from '@/hooks/use-cli-login'
import { errText } from '@/lib/errors'
import { Button } from '@/components/ui/button'
import { inputCls } from '@/pages/settings/ui'

export default function CliLoginPage() {
  const [typed, setTyped] = useState('')
  const [code, setCode] = useState('')
  const { me } = useAuthContext()
  const teamName = me?.teams?.find((t) => t.id === me?.team)?.name ?? 'this team'
  const { pending, approve, deny } = useCliLogin(code)

  const lookup = (e: FormEvent) => {
    e.preventDefault()
    setCode(typed.trim().toUpperCase())
  }

  return (
    <div className="flex min-h-[70svh] items-center justify-center px-8 py-8">
      <div className="w-full max-w-md rounded-xl border border-foreground/10 bg-linear-to-b from-foreground/3 to-transparent p-6">
        <span className="grid h-10 w-10 place-items-center rounded-lg border border-foreground/10 bg-foreground/5">
          <Terminal className="h-5 w-5 text-foreground/80" />
        </span>
        {approve.isSuccess ? (
          <>
            <h1 className="mt-4 text-lg font-semibold">The CLI is signed in</h1>
            <p className="text-muted-foreground mt-1 text-sm">
              You can close this tab and go back to your terminal.
            </p>
          </>
        ) : deny.isSuccess ? (
          <>
            <h1 className="mt-4 text-lg font-semibold">Sign-in declined</h1>
            <p className="text-muted-foreground mt-1 text-sm">Nothing was given access.</p>
          </>
        ) : !pending.data ? (
          <form onSubmit={lookup}>
            <h1 className="mt-4 text-lg font-semibold">Sign in the Skiff CLI</h1>
            <p className="text-muted-foreground mt-1 text-sm">
              Enter the code <span className="font-mono">skiff login</span> printed in your terminal.
              Only continue if you started it yourself.
            </p>
            <input
              className={inputCls + ' mt-4 text-center font-mono tracking-[0.3em] uppercase'}
              placeholder="XXXXXXXX"
              maxLength={8}
              autoComplete="off"
              autoFocus
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
            />
            {pending.isError && (
              <p className="mt-3 text-xs text-danger">
                {errText(pending.error, 'No pending sign-in with that code. It may have expired.')}
              </p>
            )}
            <div className="mt-5 flex justify-end">
              <Button size="sm" type="submit" loading={pending.isFetching} disabled={!typed.trim()}>
                Continue
              </Button>
            </div>
          </form>
        ) : (
          <>
            <h1 className="mt-4 text-lg font-semibold">Sign in the Skiff CLI?</h1>
            <p className="text-muted-foreground mt-1 text-sm">
              This terminal wants to deploy and manage projects in{' '}
              <span className="text-foreground/80">{teamName}</span>.
            </p>
            <dl className="mt-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 rounded-lg border border-foreground/8 bg-inset px-3 py-2.5 text-sm">
              <dt className="text-muted-foreground text-xs">Computer</dt>
              <dd className="font-mono text-xs">{pending.data.host || 'unknown'}</dd>
              <dt className="text-muted-foreground text-xs">IP address</dt>
              <dd className="font-mono text-xs">{pending.data.ip || 'unknown'}</dd>
              <dt className="text-muted-foreground text-xs">Started</dt>
              <dd className="font-mono text-xs">
                {new Date(pending.data.requested * 1000).toLocaleTimeString()}
              </dd>
            </dl>
            <p className="text-muted-foreground mt-3 text-xs">
              If you don't recognise this, decline it.
            </p>
            {(approve.isError || deny.isError) && (
              <p className="mt-3 text-xs text-danger">
                {errText(approve.error ?? deny.error, 'Something went wrong.')}
              </p>
            )}
            <div className="mt-5 flex justify-end gap-2">
              <Button
                variant="outline"
                size="sm"
                loading={deny.isPending}
                onClick={() => deny.mutate()}
              >
                Decline
              </Button>
              <Button size="sm" loading={approve.isPending} onClick={() => approve.mutate()}>
                Approve
              </Button>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
