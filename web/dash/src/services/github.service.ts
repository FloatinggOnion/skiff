import { BaseService } from '@/services/base.service'

export type Repo = {
  full_name: string
  name: string
  private: boolean
  default_branch: string
  clone_url: string
}

export type GithubStatus = {
  configured: boolean
  installed: boolean
  usable?: boolean
  slug?: string
  permissionsUrl?: string // set when preview comments need "Pull requests: write"
}

class GithubService extends BaseService {
  status() {
    return this.get<GithubStatus>('/github/status')
  }

  repos() {
    return this.get<Repo[]>('/github/repos')
  }
}

export const githubService = new GithubService()
