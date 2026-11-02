import { useEffect, useState } from 'react'
import { Outlet } from 'react-router'
import { useAuthContext } from '@/context/auth-context'
import { authService } from '@/services/api.service'
import { SidebarProvider, SidebarInset } from '@/components/ui/sidebar'
import AppSidebar from '@/components/app-sidebar'
import Topbar from '@/components/topbar'
import MembersDialog from '@/components/members-dialog'
import CommandPalette from '@/components/command-palette'

// The authenticated frame: a persistent sidebar + top bar around the routed page.
export default function Shell() {
  const { me, switchTeam, logout } = useAuthContext()
  const [members, setMembers] = useState(false)
  const [palette, setPalette] = useState(false)

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        setPalette((o) => !o)
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  const createTeam = async () => {
    const name = window.prompt('New team name')
    if (name && name.trim()) {
      const t = await authService.createTeam(name.trim())
      await switchTeam(t.id)
    }
  }

  return (
    <SidebarProvider>
      <AppSidebar
        me={me!}
        switchTeam={switchTeam}
        logout={logout}
        onMembers={() => setMembers(true)}
        onCreateTeam={createTeam}
        onSearch={() => setPalette(true)}
      />
      <SidebarInset className="relative bg-transparent">
        <Topbar />
        <Outlet />
      </SidebarInset>
      <MembersDialog open={members} onOpenChange={setMembers} />
      <CommandPalette open={palette} onOpenChange={setPalette} />
    </SidebarProvider>
  )
}
