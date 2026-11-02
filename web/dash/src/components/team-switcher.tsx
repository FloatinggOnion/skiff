import { Check, ChevronsUpDown, Plus, Users } from 'lucide-react'
import type { Me } from '@/services/api.service'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

function TeamAvatar({ name, className }: { name: string; className?: string }) {
  return (
    <span
      className={
        'text-foreground flex shrink-0 items-center justify-center rounded-md bg-linear-to-br from-foreground/25 to-foreground/5 font-semibold ' +
        (className ?? '')
      }
    >
      {(name || '?').charAt(0).toUpperCase()}
    </span>
  )
}

export default function TeamSwitcher({
  me,
  switchTeam,
  onMembers,
  onCreate,
}: {
  me: Me
  switchTeam: (id: string) => Promise<void>
  onMembers: () => void
  onCreate: () => void
}) {
  const current = me.teams?.find((t) => t.id === me.team)

  return (
    <DropdownMenu>
      <DropdownMenuTrigger className="hover:bg-sidebar-accent data-[state=open]:bg-sidebar-accent flex w-full items-center gap-2.5 rounded-lg border border-foreground/8 px-2 py-2 text-left transition-colors outline-none">
        <TeamAvatar name={current?.name ?? 'Team'} className="h-7 w-7 text-[11px]" />
        <div className="min-w-0 flex-1">
          <div className="truncate text-sm leading-tight font-medium">
            {current?.name ?? 'Team'}
          </div>
          <div className="text-muted-foreground truncate text-[11px] leading-tight">
            Self-hosted
          </div>
        </div>
        <ChevronsUpDown className="text-muted-foreground h-3.5 w-3.5 shrink-0" />
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-60">
        <DropdownMenuLabel className="text-muted-foreground font-mono text-[10px] tracking-wider uppercase">
          Teams
        </DropdownMenuLabel>
        {me.teams?.map((t) => (
          <DropdownMenuItem
            key={t.id}
            onClick={() => {
              if (t.id !== me.team) void switchTeam(t.id)
            }}
            className="gap-2"
          >
            <TeamAvatar name={t.name} className="h-5 w-5 text-[9px]" />
            <span className="flex-1 truncate">{t.name}</span>
            {t.id === me.team && <Check className="h-3.5 w-3.5 shrink-0" />}
          </DropdownMenuItem>
        ))}
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={onMembers} className="gap-2">
          <Users className="h-3.5 w-3.5" />
          Members
        </DropdownMenuItem>
        <DropdownMenuItem onClick={onCreate} className="text-muted-foreground gap-2">
          <Plus className="h-3.5 w-3.5" />
          Create team…
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
