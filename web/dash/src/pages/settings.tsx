import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
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
  const [params, setParams] = useSearchParams()
  const tab = params.get('tab') ?? 'account'
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

      <Tabs value={tab} onValueChange={(v) => setParams({ tab: v }, { replace: true })}>
        <TabsList variant="line" className="mb-6 w-full justify-start gap-6 border-b border-foreground/8">
          <TabsTrigger value="account">Account</TabsTrigger>
          <TabsTrigger value="team">Team</TabsTrigger>
          <TabsTrigger value="integrations">Integrations</TabsTrigger>
        </TabsList>
        <TabsContent value="account" className="flex max-w-3xl flex-col gap-4 pb-12">
          {me?.user && <ProfileSection user={me.user} onSaved={refresh} />}
          <PasswordSection />
          <TwoFactorSection />
          <DangerSection />
        </TabsContent>
        <TabsContent value="team" className="flex max-w-3xl flex-col gap-4 pb-12">
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
        </TabsContent>
        <TabsContent value="integrations" className="flex max-w-3xl flex-col gap-4 pb-12">
          <TokensSection />
          <ConnectionsSection gh={gh} />
        </TabsContent>
      </Tabs>
    </div>
  )
}
