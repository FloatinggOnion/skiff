import { BaseService } from '@/services/base.service'

export type User = { id: string; email: string; name: string; created?: number }
export type Team = { id: string; name: string; slug: string; created: number }
export type Member = { user: User; role: string }
export type Me = {
  authenticated: boolean
  needsSetup: boolean
  user?: User
  teams?: Team[]
  team?: string
  role?: string
  admin?: boolean
}

// A sign-in that still needs a code from the user's authenticator app; empty when the password was enough.
export type SignInStep = { twoFactor: true; ticket: string } | '' | undefined

export type TwoFactorStatus = { enabled: boolean; recoveryLeft: number }

class AuthService extends BaseService {
  me() {
    return this.get<Me>('/me')
  }

  setup(secret: string, email: string, name: string, password: string) {
    return this.post('/auth/setup', { secret, email, name, password })
  }

  login(email: string, password: string) {
    return this.post<SignInStep>('/auth/login', { email, password })
  }

  loginCode(ticket: string, code: string) {
    return this.post('/auth/login/code', { ticket, code })
  }

  logout() {
    return this.post('/auth/logout')
  }

  accept(token: string, name: string, password: string) {
    return this.post<SignInStep>('/auth/accept', { token, name, password })
  }

  switchTeam(team: string) {
    return this.post('/auth/team', { team })
  }

  createTeam(name: string) {
    return this.post<Team>('/teams', { name })
  }

  members() {
    return this.get<Member[]>('/teams/members')
  }

  invite(email: string, role: string) {
    return this.post<{ link: string; emailed?: boolean }>('/teams/invite', { email, role })
  }

  removeMember(userId: string) {
    return this.delete('/teams/members', { params: { user: userId } })
  }

  updateProfile(name: string) {
    return this.post('/account', { name })
  }

  changePassword(current: string, password: string) {
    return this.post('/account/password', { current, password })
  }

  twoFactor() {
    return this.get<TwoFactorStatus>('/account/2fa')
  }

  twoFactorSetup(password: string) {
    return this.post<{ secret: string; uri: string }>('/account/2fa', {
      action: 'setup',
      password,
    })
  }

  twoFactorEnable(code: string) {
    return this.post<{ recoveryCodes: string[] }>('/account/2fa', { action: 'enable', code })
  }

  twoFactorDisable(password: string, code: string) {
    return this.post('/account/2fa', { action: 'disable', password, code })
  }

  twoFactorRecovery(password: string, code: string) {
    return this.post<{ recoveryCodes: string[] }>('/account/2fa', {
      action: 'recovery',
      password,
      code,
    })
  }

  renameTeam(name: string) {
    return this.post('/teams/rename', { name })
  }

  leaveTeam() {
    return this.post('/teams/leave')
  }

  deleteTeam() {
    return this.post('/teams/delete')
  }

  deleteAccount(password: string, code?: string) {
    return this.post('/account/delete', { password, code })
  }
}

export const authService = new AuthService()
