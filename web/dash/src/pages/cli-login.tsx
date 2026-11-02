import { useSearchParams } from 'react-router'
import { Terminal } from 'lucide-react'
import { useAuthContext } from '@/context/auth-context'
import { useCliLogin } from '@/hooks/use-cli-login'
import { errText } from '@/lib/errors'
import { Button } from '@/components/ui/button'

export default function CliLoginPage() {
  const [params] = useSearchParams()
  const code = (params.get('code') ?? '').toUpperCase()
  const { me } = useAuthContext()
  const teamName = me?.teams?.find((t) => t.id === me?.team)?.name ?? 'this team'
  const { pending, approve, deny } = useCliLogin(code)

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
        ) : pending.isError || !code ? (
          <>
            <h1 className="mt-4 text-lg font-semibold">No sign-in to approve</h1>
            <p className="text-muted-foreground mt-1 text-sm">
              It may have expired. Run <span className="font-mono">skiff login</span> again.
            </p>
          </>
        ) : (
          <>
            <h1 className="mt-4 text-lg font-semibold">Sign in the Skiff CLI?</h1>
            <p className="text-muted-foreground mt-1 text-sm">
              {pending.data?.host ? (
                <>
                  <span className="text-foreground/80 font-mono">{pending.data.host}</span> wants to
                  deploy and manage projects in{' '}
                </>
              ) : (
                'A terminal wants to deploy and manage projects in '
              )}
              <span className="text-foreground/80">{teamName}</span>.
            </p>
            <div className="mt-4 rounded-lg border border-foreground/8 bg-inset px-3 py-2.5 text-center">
              <p className="text-muted-foreground text-[11px]">Check it matches your terminal</p>
              <p className="mt-1 font-mono text-xl tracking-[0.3em]">{code}</p>
            </div>
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
