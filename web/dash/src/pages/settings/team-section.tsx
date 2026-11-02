import { useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { authService, type Team } from '@/services/api.service'
import { errText } from '@/lib/errors'
import { useConfirm } from '@/providers/confirm-provider'
import { Section, Field, SaveButton, inputCls } from './ui'

export function TeamSection({
  team,
  isOwner,
  onSaved,
}: {
  team?: Team
  isOwner: boolean
  onSaved: () => Promise<unknown>
}) {
  const [name, setName] = useState(team?.name ?? '')
  const confirm = useConfirm()
  const dirty = name.trim() !== '' && name.trim() !== (team?.name ?? '')
  const verb = isOwner ? 'Delete' : 'Leave'

  const rename = useMutation({
    mutationFn: () => authService.renameTeam(name.trim()),
    onSuccess: () => onSaved(),
  })

  const danger = useMutation({
    mutationFn: () => (isOwner ? authService.deleteTeam() : authService.leaveTeam()),
    onSuccess: () => {
      window.location.href = '/'
    },
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    rename.mutate()
  }

  const leaveOrDelete = async () => {
    if (
      !(await confirm({
        title: `${verb} ${team?.name ?? 'this team'}?`,
        description: "This can't be undone.",
        confirmText: verb,
        destructive: true,
      }))
    )
      return
    danger.mutate()
  }

  return (
    <Section title="Team" description="Projects, databases, and domains are scoped to a team.">
      <form onSubmit={submit} className="space-y-3">
        <Field label="Name">
          <input
            className={inputCls}
            value={name}
            disabled={!isOwner}
            onChange={(e) => {
              setName(e.target.value)
              rename.reset()
            }}
          />
        </Field>
        <div className="flex items-center justify-between gap-3">
          <span className="text-muted-foreground text-xs">
            Slug <span className="text-foreground/70 font-mono">{team?.slug}</span>
          </span>
          {isOwner ? (
            <SaveButton busy={rename.isPending} saved={rename.isSuccess} disabled={!dirty} />
          ) : (
            <span className="text-muted-foreground text-xs">Only owners can rename the team.</span>
          )}
        </div>
      </form>

      <div className="mt-4 flex items-center justify-between gap-3 border-t border-foreground/5 pt-4">
        <p className="text-muted-foreground text-[11px]">
          {isOwner
            ? 'Deleting removes the team for everyone. Remove its projects and databases first.'
            : "You'll lose access to this team's projects."}
        </p>
        <button
          type="button"
          onClick={leaveOrDelete}
          disabled={danger.isPending}
          className="inline-flex shrink-0 items-center gap-1.5 rounded-[6px] border border-danger/30 px-2.5 py-1 text-xs text-danger transition hover:bg-danger/10 disabled:opacity-50"
        >
          {danger.isPending && <Loader2 className="h-3 w-3 animate-spin" />}
          {isOwner ? 'Delete team' : 'Leave team'}
        </button>
      </div>
      {rename.isError && (
        <p className="mt-2 text-xs text-danger">
          {errText(rename.error, 'Could not rename the team.')}
        </p>
      )}
      {danger.isError && (
        <p className="mt-2 text-xs text-danger">
          {errText(danger.error, `Could not ${verb.toLowerCase()} the team.`)}
        </p>
      )}
    </Section>
  )
}
