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
    try {
      const r = await authService.invite(email.trim(), 'member')
      setLink(r.link)
      setSent({ to: email.trim(), emailed: r.emailed })
      authService
        .members()
        .then(setMembers)
        .catch(() => {})
    } catch (e) {
      setError(errText(e, 'Could not create invite'))
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Team members</DialogTitle>
          <DialogDescription>
            Everyone here can see and manage this team's projects.
          </DialogDescription>
        </DialogHeader>

        <div className="rounded-md border">
          {members.length === 0 ? (
            <p className="text-muted-foreground p-4 text-center text-sm">…</p>
          ) : (
            members.map((m) => (
              <div
                key={m.user.id}
                className="flex items-center justify-between border-b px-4 py-2.5 text-sm last:border-0"
              >
                <span className="truncate">
                  {m.user.name} <span className="text-muted-foreground">· {m.user.email}</span>
                </span>
                <span className="text-muted-foreground font-mono text-[11px] uppercase">
                  {m.role}
                </span>
              </div>
            ))
          )}
        </div>

        <div className="grid gap-2">
          <Label htmlFor="invite">Invite by email</Label>
          <div className="flex gap-2">
            <Input
              id="invite"
              type="email"
              placeholder="teammate@company.com"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
            />
            <Button onClick={invite}>Invite</Button>
          </div>
          {error && <p className="text-destructive text-xs">{error}</p>}
          {link && (
            <div className="bg-muted/30 rounded-md border p-2.5">
              <p className="text-muted-foreground mb-1 text-xs">{inviteNote(sent)}</p>
              <code className="text-xs break-all">{link}</code>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
