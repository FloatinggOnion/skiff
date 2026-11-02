import { useNavigate } from 'react-router'
import {
  LayoutGrid,
  Rocket,
  ScrollText,
  Server,
  Globe,
  Mail,
  KeyRound,
  Settings,
  Box,
  GitBranch,
} from 'lucide-react'
import { useApps } from '@/hooks/use-apps'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'

const NAV = [
  { to: '/', label: 'Projects', icon: LayoutGrid },
  { to: '/deployments', label: 'Deployments', icon: Rocket },
  { to: '/logs', label: 'Logs', icon: ScrollText },
  { to: '/server', label: 'Server', icon: Server },
  { to: '/domains', label: 'Domains', icon: Globe },
  { to: '/email', label: 'Email', icon: Mail },
  { to: '/env', label: 'Environment', icon: KeyRound },
  { to: '/settings', label: 'Settings', icon: Settings },
]

export default function CommandPalette({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const navigate = useNavigate()
  const { apps } = useApps()

  const go = (to: string) => {
    onOpenChange(false)
    navigate(to)
  }

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title="Command menu"
      description="Search or jump to a page"
    >
      <CommandInput placeholder="Search or jump to…" />
      <CommandList>
        <CommandEmpty>No results.</CommandEmpty>
        <CommandGroup heading="Navigate">
          {NAV.map((n) => (
            <CommandItem key={n.to} value={n.label} onSelect={() => go(n.to)}>
              <n.icon />
              {n.label}
            </CommandItem>
          ))}
        </CommandGroup>
        {apps.length > 0 && (
          <CommandGroup heading="Projects">
            {apps.map((a) => (
              <CommandItem key={a.name} value={'project ' + a.name} onSelect={() => go('/')}>
                <Box />
                {a.name}
              </CommandItem>
            ))}
          </CommandGroup>
        )}
        <CommandGroup heading="Actions">
          <CommandItem value="new project deploy from git image" onSelect={() => go('/')}>
            <GitBranch />
            New project
          </CommandItem>
        </CommandGroup>
      </CommandList>
    </CommandDialog>
  )
}
