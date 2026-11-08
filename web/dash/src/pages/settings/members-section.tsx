import { useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Trash2, X } from 'lucide-react'
import { authService, type Member } from '@/services/api.service'
import { errText } from '@/lib/errors'
import { Button } from '@/components/ui/button'
import { useConfirm } from '@/providers/confirm-provider'
import { Section, inputCls } from './ui'
import { inviteNote, type InviteSent } from '@/lib/invites'

export function MembersSection({
  members,
  meId,
  isOwner,
  onChange,
}: {
  members: Member[]
  meId: string
  isOwner: boolean
  onChange: () => void
}) {
  const [email, setEmail] = useState('')
  const [link, setLink] = useState('')
  const [sent, setSent] = useState<InviteSent>({ to: '' })
  const confirm = useConfirm()

  const inviteMut = useMutation({
    mutationFn: () => authService.invite(email.trim(), 'member'),
    onSuccess: (r) => {
      setLink(r.link)
      setSent({ to: email.trim(), emailed: r.emailed })
      setEmail('')
      onChange()
    },
  })

  const removeMut = useMutation({
    mutationFn: (id: string) => authService.removeMember(id),
    onSuccess: () => onChange(),
  })

  const submitInvite = (e: FormEvent) => {
    e.preventDefault()
    setLink('')
    inviteMut.mutate()
  }

  const remove = async (m: Member) => {
    if (
      !(await confirm({
        title: `Remove ${m.user.name}?`,
        description: 'They lose access to this team.',
        confirmText: 'Remove',
        destructive: true,
      }))
    )
      return
    removeMut.mutate(m.user.id)
  }

  return (
    <Section title="Members" description="Everyone here can see and manage this team's projects.">
      <div className="divide-y divide-foreground/5 overflow-hidden rounded-lg border border-foreground/8">
        {members.map((m) => (
          <div key={m.user.id} className="flex items-center gap-3 px-3.5 py-2.5">
            <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full border border-foreground/10 bg-foreground/5 text-xs font-medium uppercase">
              {m.user.name.charAt(0)}
            </span>
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm">
                {m.user.name}
                {m.user.id === meId && <span className="text-muted-foreground"> (you)</span>}
              </p>
              <p className="text-muted-foreground truncate text-xs">{m.user.email}</p>
            </div>
            <span
              className={
                'rounded-full border px-2 py-0.5 font-mono text-[10px] uppercase ' +
                (m.role === 'owner'
                  ? 'text-foreground/80 border-foreground/15'
                  : 'text-muted-foreground border-foreground/10')
              }
            >
              {m.role}
            </span>
            {isOwner && m.user.id !== meId && (
              <button
                onClick={() => remove(m)}
                className="text-muted-foreground p-1 transition hover:text-danger"
                title={`Remove ${m.user.name}`}
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        ))}
      </div>

      {isOwner && (
        <form onSubmit={submitInvite} className="mt-3 space-y-2">
          <div className="flex gap-2">
            <input
              className={inputCls}
              type="email"
              placeholder="teammate@company.com"
              value={email}
              onChange={(e) => {
                setEmail(e.target.value)
                inviteMut.reset()
              }}
            />
            <Button
              type="submit"
              size="sm"
              loading={inviteMut.isPending}
              disabled={!email.trim()}
              className="shrink-0"
            >
              Invite
            </Button>
          </div>
          {link && <p className="text-muted-foreground text-xs">{inviteNote(sent)}</p>}
          {link && (
            <div className="flex items-center gap-2 rounded-[6px] border border-foreground/8 bg-inset px-3 py-2">
              <span className="text-foreground/80 min-w-0 flex-1 truncate font-mono text-xs">
                {link}
              </span>
              <button
                type="button"
                onClick={() => navigator.clipboard?.writeText(link)}
                className="text-muted-foreground hover:text-foreground shrink-0 text-xs"
              >
                Copy
              </button>
              <button
                type="button"
                onClick={() => setLink('')}
                className="text-muted-foreground hover:text-foreground shrink-0"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          )}
        </form>
      )}
      {inviteMut.isError && (
        <p className="mt-2 text-xs text-danger">
          {errText(inviteMut.error, 'Could not create an invite.')}
        </p>
      )}
      {removeMut.isError && (
        <p className="mt-2 text-xs text-danger">
          {errText(removeMut.error, 'Could not remove that member.')}
        </p>
      )}
    </Section>
  )
}
