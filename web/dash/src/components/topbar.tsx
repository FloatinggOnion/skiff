import { useLocation } from 'react-router'
import { SidebarTrigger } from '@/components/ui/sidebar'
import NotificationBell from '@/components/notification-bell'

const titles: Record<string, string> = {
  '': 'Projects',
  projects: 'Projects',
  deployments: 'Deployments',
  databases: 'Databases',
  stacks: 'Stacks',
  storage: 'Storage',
  domains: 'Domains',
  email: 'Email',
  env: 'Environment',
  logs: 'Logs',
  analytics: 'Analytics',
  activity: 'Activity',
  server: 'Server',
  settings: 'Settings',
}

export default function Topbar() {
  const { pathname } = useLocation()
  const title = titles[pathname.split('/')[1]] ?? 'Skiff'
  return (
    <header className="border-border bg-background/70 sticky top-0 z-30 flex h-14 shrink-0 items-center gap-2 border-b px-4 backdrop-blur-xl">
      <SidebarTrigger className="text-muted-foreground -ml-1" />
      <span className="text-sm font-medium">{title}</span>
      <div className="ml-auto">
        <NotificationBell />
      </div>
    </header>
  )
}
