import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldLabel } from '@/components/ui/field'
import { PasswordInput } from '@/components/ui/password-input'
import { Textarea } from '@/components/ui/textarea'
import { queryKeys } from '@/constants/query-keys'
import { errText } from '@/lib/errors'
import { stacksService, type Stack, type StackChanges, type StackRequest } from '@/services/api.service'

const actionLabel = { add: 'Add', update: 'Change', remove: 'Remove' } as const

export function StackUpdateDialog({
  stack,
  open,
  onOpenChange,
}: {
  stack: Stack
  open: boolean
  onOpenChange: (v: boolean) => void
}) {
  const qc = useQueryClient()
  const [compose, setCompose] = useState('')
  const [vars, setVars] = useState<Record<string, string>>({})
  const [askFor, setAskFor] = useState<string[]>([])
  const [result, setResult] = useState<StackChanges | null>(null)
  const [allow, setAllow] = useState(false)
  const [checking, setChecking] = useState(false)
  const [applying, setApplying] = useState(false)
  const [error, setError] = useState('')

  const request = (): StackRequest =>
    stack.fromGit ? { name: stack.name, vars } : { name: stack.name, compose, vars }

  const check = async () => {
    setError('')
    setChecking(true)
    setAllow(false)
    try {
      const r = await stacksService.changes(request())
      setResult(r)
      setAskFor((prev) => [...new Set([...prev, ...r.unset])])
    } catch (err) {
      setResult(null)
      setError(errText(err, 'Could not read the Compose file.'))
    } finally {
      setChecking(false)
    }
  }

  useEffect(() => {
    if (open && stack.fromGit) void check()
    if (!open) {
      setCompose('')
      setVars({})
      setAskFor([])
      setResult(null)
      setAllow(false)
      setError('')
    }
    // Only when the dialog opens or closes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open])

  const apply = async () => {
    setError('')
    setApplying(true)
    try {
      await stacksService.update({ ...request(), allowDataLoss: allow })
      qc.invalidateQueries({ queryKey: queryKeys.stacks })
      onOpenChange(false)
    } catch (err) {
      setError(errText(err, 'Could not apply the changes. Check the file again to see what changed.'))
    } finally {
      setApplying(false)
    }
  }

  const blocked = !result || result.problems.length > 0 || result.changes.length === 0 || (result.deletesData && !allow)

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="font-mono text-sm">Update {stack.name}</DialogTitle>
        </DialogHeader>
        <div className="flex flex-col gap-4">
          {stack.fromGit ? (
            <p className="text-muted-foreground text-sm">
              Compares the Compose file in your repository with what's running. Safe changes apply when you
              push, so this is for changes that need your confirmation.
            </p>
          ) : (
            <Field>
              <FieldLabel htmlFor="stack-update-compose">Updated Compose file</FieldLabel>
              <Textarea
                id="stack-update-compose"
                rows={10}
                spellCheck={false}
                className="font-mono text-xs"
                placeholder="services:"
                value={compose}
                onChange={(e) => setCompose(e.target.value)}
              />
            </Field>
          )}

          {askFor.length > 0 && (
            <div className="flex flex-col gap-3">
              <p className="text-sm font-medium">New values this file needs</p>
              {askFor.map((k) => (
                <Field key={k}>
                  <FieldLabel htmlFor={`update-var-${k}`} className="font-mono text-xs">
                    {k}
                  </FieldLabel>
                  <PasswordInput
                    id={`update-var-${k}`}
                    autoComplete="off"
                    value={vars[k] ?? ''}
                    onChange={(e) => setVars((prev) => ({ ...prev, [k]: e.target.value }))}
                  />
                </Field>
              ))}
            </div>
          )}

          {result && result.changes.length === 0 && result.problems.length === 0 && (
            <p className="rounded-lg border border-foreground/8 p-4 text-center text-sm">
              Everything already matches this file.
            </p>
          )}

          {result && result.changes.length > 0 && (
            <div className="divide-y divide-foreground/5 overflow-hidden rounded-lg border border-foreground/8">
              {result.changes.map((c) => (
                <div key={c.kind + c.service + c.action} className="p-3">
                  <p className="text-sm">
                    <span className={c.action === 'remove' ? 'text-danger' : 'text-muted-foreground'}>
                      {actionLabel[c.action]}
                    </span>{' '}
                    <span className="font-mono">{c.service}</span>{' '}
                    <span className="text-muted-foreground text-xs">{c.kind}</span>
                    {c.deletesData && <span className="text-xs text-danger"> · deletes data</span>}
                  </p>
                  {c.details?.map((d) => (
                    <p key={d} className="text-muted-foreground mt-0.5 text-xs">
                      {d}
                    </p>
                  ))}
                </div>
              ))}
            </div>
          )}

          {result && result.problems.length > 0 && (
            <div className="rounded-lg border border-danger/25 bg-danger/5 p-3">
              <p className="text-sm font-medium text-danger">Fix these before updating</p>
              <ul className="mt-1.5 flex flex-col gap-1 text-xs text-danger">
                {result.problems.map((p) => (
                  <li key={p}>{p}</li>
                ))}
              </ul>
            </div>
          )}

          {result && result.notes.length > 0 && (
            <div className="rounded-lg border border-foreground/8 p-3">
              <p className="text-sm font-medium">Worth knowing</p>
              <ul className="text-muted-foreground mt-1.5 flex flex-col gap-1 text-xs">
                {result.notes.map((n) => (
                  <li key={n}>{n}</li>
                ))}
              </ul>
            </div>
          )}

          {result?.deletesData && (
            <label className="flex items-start gap-2.5 rounded-lg border border-danger/25 bg-danger/5 p-3 text-sm">
              <input
                type="checkbox"
                className="mt-0.5"
                checked={allow}
                onChange={(e) => setAllow(e.target.checked)}
              />
              <span>
                I understand this deletes a database or volume, its data and its backups from this server.
                Copies in an offsite bucket stay. Download a backup first if you might need it.
              </span>
            </label>
          )}

          {error && <p className="text-xs text-danger">{error}</p>}

          <div className="flex items-center justify-end gap-2">
            <Button size="sm" variant={result ? 'outline' : 'default'} onClick={check} loading={checking}>
              {result ? 'Check again' : 'Check for changes'}
            </Button>
            {result && result.changes.length > 0 && (
              <Button size="sm" onClick={apply} loading={applying} disabled={blocked}>
                Apply changes
              </Button>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
