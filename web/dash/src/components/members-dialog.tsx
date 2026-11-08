import { useEffect, useState } from 'react'
import { authService, type Member } from '@/services/api.service'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { errText } from '@/lib/errors'
import { inviteNote, type InviteSent } from '@/lib/invites'

export default function MembersDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [members, setMembers] = useState<Member[]>([])
  const [email, setEmail] = useState('')
  const [link, setLink] = useState('')
  const [sent, setSent] = useState<InviteSent>({ to: '' })
  const [error, setError] = useState('')
  const [inviting, setInviting] = useState(false)

  useEffect(() => {
    if (open) {
      setLink('')
      setEmail('')
      setError('')
      authService
        .members()
        .then(setMembers)
        .catch(() => setMembers([]))
    }
  }, [open])

  const invite = async () => {
    setError('')
    setInviting(true)
    try {
      const r = await authService.invite(email.trim(), 'member')
      setLink(r.link)
      setSent({ to: email.trim(), emailed: r.emailed })
      setEmail('')
      authService
        .members()
        .then(setMembers)
        .catch(() => {})
    } catch (e) {
      setError(errText(e, 'Could not create invite'))
    } finally {
      setInviting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Team members</DialogTitle>
          <DialogDescription>
            Everyone here can see and manage this team's projects.
          </DialogDescription>
        </DialogHeader>

        <div className="divide-foreground/5 border-foreground/8 min-w-0 divide-y overflow-hidden rounded-lg border">
          {members.length === 0 ? (
            <p className="text-muted-foreground p-4 text-center text-sm">…</p>
          ) : (
            members.map((m) => (
              <div key={m.user.id} className="flex items-center gap-3 px-3.5 py-2.5">
                <span className="border-foreground/10 bg-foreground/5 grid h-8 w-8 shrink-0 place-items-center rounded-full border text-xs font-medium uppercase">
                  {m.user.name.charAt(0)}
                </span>
                <div className="min-w-0 flex-1">
                  <p className="truncate text-sm">{m.user.name}</p>
                  <p className="text-muted-foreground truncate text-xs">{m.user.email}</p>
                </div>
                <span
                  className={
                    'shrink-0 rounded-full border px-2 py-0.5 font-mono text-[10px] uppercase ' +
                    (m.role === 'owner'
                      ? 'text-foreground/80 border-foreground/15'
                      : 'text-muted-foreground border-foreground/10')
                  }
                >
                  {m.role}
                </span>
              </div>
            ))
          )}
        </div>

        <form
          className="grid min-w-0 gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            void invite()
          }}
        >
          <Label htmlFor="invite">Invite by email</Label>
          <div className="flex gap-2">
            <Input
              id="invite"
              type="email"
              placeholder="teammate@company.com"
              className="min-w-0 flex-1"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
            <Button type="submit" className="shrink-0" loading={inviting} disabled={!email.trim()}>
              Invite
            </Button>
          </div>
          {error && <p className="text-destructive text-xs">{error}</p>}
          {link && (
            <div className="bg-muted/30 rounded-md border p-2.5">
              <p className="text-muted-foreground mb-1 text-xs">{inviteNote(sent)}</p>
              <div className="flex items-start gap-2">
                <code className="min-w-0 flex-1 text-xs break-all">{link}</code>
                <button
                  type="button"
                  onClick={() => navigator.clipboard?.writeText(link)}
                  className="text-muted-foreground hover:text-foreground shrink-0 cursor-pointer text-xs"
                >
                  Copy
                </button>
              </div>
            </div>
          )}
        </form>
      </DialogContent>
    </Dialog>
  )
}
