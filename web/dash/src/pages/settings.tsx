import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useAuthContext } from '@/context/auth-context'
import { authService, githubService, type Member, type GithubStatus } from '@/services/api.service'
import { queryKeys } from '@/constants/query-keys'
import { ProfileSection } from './settings/profile-section'
import { PasswordSection } from './settings/password-section'
import { TwoFactorSection } from './settings/two-factor-section'
import { TeamSection } from './settings/team-section'
import { MembersSection } from './settings/members-section'
import { AlertsSection } from './settings/alerts-section'
import { LogDrainSection } from './settings/log-drain-section'
import { OffsiteSection } from './settings/offsite-section'
import { TokensSection } from './settings/tokens-section'
import { ConnectionsSection } from './settings/connections-section'
import { DangerSection } from './settings/danger-section'

export default function SettingsPage() {
  const { me, refresh } = useAuthContext()
  const qc = useQueryClient()
  const isOwner = me?.role === 'owner'
  const team = me?.teams?.find((t) => t.id === me?.team)

  const { data: members = [] } = useQuery<Member[]>({
    queryKey: queryKeys.members,
    queryFn: () => authService.members(),
  })
  const { data: gh } = useQuery<GithubStatus>({
    queryKey: queryKeys.github.status,
    queryFn: () => githubService.status(),
  })
  const reloadMembers = () => qc.invalidateQueries({ queryKey: queryKeys.members })

  return (
    <div className="px-8 py-8">
      <header className="mb-6">
        <h1 className="text-xl font-semibold tracking-tight">Settings</h1>
        <p className="text-muted-foreground mt-1 text-sm">Your account, team, and connections.</p>
      </header>

      <div className="grid gap-4 xl:grid-cols-2 xl:items-start">
        {me?.user && <ProfileSection user={me.user} onSaved={refresh} />}
        <PasswordSection />
        <TwoFactorSection />
        <TeamSection team={team} isOwner={isOwner} onSaved={refresh} />
        <MembersSection
          members={members}
          meId={me?.user?.id ?? ''}
          isOwner={isOwner}
          onChange={reloadMembers}
        />
        <AlertsSection />
        {isOwner && <LogDrainSection />}
        <OffsiteSection isOwner={isOwner} />
        <TokensSection />
        <ConnectionsSection gh={gh} />
        <DangerSection />
      </div>
    </div>
  )
}
