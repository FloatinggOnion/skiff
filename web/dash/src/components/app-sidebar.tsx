import type { ComponentType } from 'react'
import { NavLink, useLocation } from 'react-router'
import {
  Layers,
  LayoutGrid,
  Rocket,
  ScrollText,
  Activity,
  History,
  Server,
  Globe,
  Mail,
  Database,
  Boxes,
  KeyRound,
  Settings,
  Search,
} from 'lucide-react'
import type { Me } from '@/services/api.service'
import { Logo } from '@/components/logo'
import { ServerGauge } from '@/components/server-gauge'
import TeamSwitcher from '@/components/team-switcher'
import UserMenu from '@/components/user-menu'
import {
  Sidebar,
  SidebarHeader,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
} from '@/components/ui/sidebar'

type NavItem = {
  to: string
  label: string
  icon: ComponentType<{ className?: string }>
  end?: boolean
}

const SECTIONS: { label?: string; items: NavItem[] }[] = [
  {
    label: 'Ship',
    items: [
      { to: '/', label: 'Projects', icon: LayoutGrid, end: true },
      { to: '/stacks', label: 'Stacks', icon: Layers },
      { to: '/deployments', label: 'Deployments', icon: Rocket },
    ],
  },
  {
    label: 'Run',
    items: [
      { to: '/databases', label: 'Databases', icon: Database },
      { to: '/storage', label: 'Storage', icon: Boxes },
      { to: '/domains', label: 'Domains', icon: Globe },
      { to: '/email', label: 'Email', icon: Mail },
      { to: '/env', label: 'Environment', icon: KeyRound },
    ],
  },
  {
    label: 'Watch',
    items: [
      { to: '/logs', label: 'Logs', icon: ScrollText },
      { to: '/analytics', label: 'Analytics', icon: Activity },
      { to: '/activity', label: 'Activity', icon: History },
    ],
  },
  {
    items: [
      { to: '/server', label: 'Server', icon: Server },
      { to: '/settings', label: 'Settings', icon: Settings },
    ],
  },
]

export default function AppSidebar({
  me,
  switchTeam,
  logout,
  onMembers,
  onCreateTeam,
  onSearch,
}: {
  me: Me
  switchTeam: (id: string) => Promise<void>
  logout: () => void
  onMembers: () => void
  onCreateTeam: () => void
  onSearch: () => void
}) {
  const { pathname } = useLocation()
  const active = (to: string, end?: boolean) =>
    end ? pathname === to : pathname === to || pathname.startsWith(to + '/')

  return (
    <Sidebar>
      <SidebarHeader className="gap-3">
        <div className="flex items-center px-2 pt-1">
          <Logo />
        </div>
        <TeamSwitcher
          me={me}
          switchTeam={switchTeam}
          onMembers={onMembers}
          onCreate={onCreateTeam}
        />
        <button
          onClick={onSearch}
          className="hover:bg-sidebar-accent text-muted-foreground flex w-full items-center gap-2 rounded-lg border border-foreground/8 px-2.5 py-2 text-sm transition-colors"
        >
          <Search className="h-3.5 w-3.5 shrink-0" />
          <span className="flex-1 text-left">Search…</span>
          <kbd className="rounded border border-foreground/12 bg-foreground/5 px-1.5 py-0.5 font-mono text-[10px]">
            ⌘K
          </kbd>
        </button>
      </SidebarHeader>

      <SidebarContent>
        {SECTIONS.map((section, i) => (
          <SidebarGroup key={section.label ?? i} className="py-1">
            {section.label && (
              <SidebarGroupLabel className="h-6 font-mono text-[10px] tracking-wider uppercase">
                {section.label}
              </SidebarGroupLabel>
            )}
            <SidebarGroupContent>
              <SidebarMenu className="gap-0.5">
                {section.items
                  .filter((n) => n.to !== '/server' || me.admin)
                  .map((n) => (
                  <SidebarMenuItem key={n.to}>
                    <SidebarMenuButton
                      asChild
                      isActive={active(n.to, n.end)}
                      className="h-8 gap-2.5"
                    >
                      <NavLink to={n.to} end={n.end}>
                        <n.icon />
                        <span>{n.label}</span>
                      </NavLink>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>

      <SidebarFooter className="gap-1">
        <ServerGauge />
        <UserMenu me={me} logout={logout} />
      </SidebarFooter>
    </Sidebar>
  )
}
