import type { ComponentType } from 'react'
import { Fragment } from 'react'
import { NavLink, useLocation } from 'react-router'
import {
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
import TeamSwitcher from '@/components/team-switcher'
import UserMenu from '@/components/user-menu'
import {
  Sidebar,
  SidebarHeader,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarMenu,
  SidebarMenuItem,
  SidebarMenuButton,
  SidebarSeparator,
} from '@/components/ui/sidebar'

type NavItem = {
  to: string
  label: string
  icon: ComponentType<{ className?: string }>
  end?: boolean
}

const SECTIONS: NavItem[][] = [
  [
    { to: '/', label: 'Projects', icon: LayoutGrid, end: true },
    { to: '/deployments', label: 'Deployments', icon: Rocket },
    { to: '/logs', label: 'Logs', icon: ScrollText },
    { to: '/analytics', label: 'Analytics', icon: Activity },
    { to: '/activity', label: 'Activity', icon: History },
  ],
  [
    { to: '/server', label: 'Server', icon: Server },
    { to: '/domains', label: 'Domains', icon: Globe },
    { to: '/email', label: 'Email', icon: Mail },
    { to: '/databases', label: 'Databases', icon: Database },
    { to: '/storage', label: 'Storage', icon: Boxes },
    { to: '/env', label: 'Environment', icon: KeyRound },
  ],
  [{ to: '/settings', label: 'Settings', icon: Settings }],
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
          <Fragment key={i}>
            {i > 0 && <SidebarSeparator className="mx-3 my-1.5" />}
            <SidebarGroup className="py-1.5">
              <SidebarGroupContent>
                <SidebarMenu className="gap-1.5">
                  {section.map((n) => (
                    <SidebarMenuItem key={n.to}>
                      <SidebarMenuButton
                        asChild
                        isActive={active(n.to, n.end)}
                        className="h-9 gap-2.5"
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
          </Fragment>
        ))}
      </SidebarContent>

      <SidebarFooter>
        <UserMenu me={me} logout={logout} />
      </SidebarFooter>
    </Sidebar>
  )
}
