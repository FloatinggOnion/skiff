import { BaseService } from '@/services/base.service'
import type { Deploy } from '@/services/deploys.service'

export type App = {
  name: string
  state: string
  url: string
  repo?: string
  branch?: string
  auto: boolean
  commit?: string
  message?: string
  updated?: number
  origin?: 'cli'
  image?: string
  domain?: string
}

export type ProjectDetail = {
  name: string
  state: string
  url: string
  repo: string
  branch: string
  rootDir: string
  port: string
  auto: boolean
  previewAuto: boolean
  replicas: number
  running: number
  release: string
  autoscale: boolean
  scaleMin: number
  scaleMax: number
  scaleCpu: number
  deploys: Deploy[]
  previews: Preview[]
  origin?: 'cli'
  sleepAfter: number
  sleeping: boolean
  previewProtected: boolean
  healthPath: string
  healthTimeout: number
  image?: string
  maxReplicas: number
  memory: string
  cpu: string
  domains: string[]
}

export type RegistryCreds = {
  username: string
  set: boolean
  host: string
}

export type ImageProject = {
  name: string
  image: string
  port: string
  username: string
  password: string
}

export type Job = {
  id: string
  app: string
  name: string
  schedule: string
  command: string
  lastRun: number
  lastOk: boolean
  next: number
  created: number
}

export type Worker = {
  id: string
  app: string
  name: string
  command: string
  replicas: number
  running: number
  created: number
}

export type Volume = {
  name: string
  path: string
  size: number
  attached: boolean
}

export type Volumes = {
  kind: 'project' | 'cli' | 'preview'
  volumes: Volume[]
}

export type Preview = {
  name: string
  branch: string
  url: string
  state: string
  status: string
  updated: number
}

export type ProjectSettings = {
  branch: string
  rootDir: string
  port: string
  auto: boolean
  previewAuto: boolean
  replicas: number
  release: string
  autoscale: boolean
  scaleMin: number
  scaleMax: number
  scaleCpu: number
  sleepAfter: number
  healthPath: string
  healthTimeout: number
  image?: string
  memory: string
  cpu: string
}

class ProjectsService extends BaseService {
  list() {
    return this.get<App[]>('/apps')
  }

  detail(name: string) {
    return this.get<ProjectDetail>('/project', { params: { app: name } })
  }

  update(name: string, settings: ProjectSettings) {
    return this.put('/project', settings, { params: { app: name } })
  }

  createFromImage(project: ImageProject) {
    return this.post<{ app: string; id: string }>('/projects/image', project)
  }

  registry(name: string) {
    return this.get<RegistryCreds>('/project/registry', { params: { app: name } })
  }

  setRegistry(name: string, username: string, password: string) {
    return this.put('/project/registry', { username, password }, { params: { app: name } })
  }

  removeRegistry(name: string) {
    return this.delete('/project/registry', { params: { app: name } })
  }

  setPreviewPassword(name: string, password: string) {
    return this.put('/project/preview-password', { password }, { params: { app: name } })
  }

  removePreviewPassword(name: string) {
    return this.delete('/project/preview-password', { params: { app: name } })
  }

  stop(name: string) {
    return this.post('/down', null, { params: { app: name } })
  }

  jobs(app: string) {
    return this.get<Job[]>('/jobs', { params: { app } })
  }

  createJob(app: string, name: string, schedule: string, command: string) {
    return this.post<Job>('/jobs', { name, schedule, command }, { params: { app } })
  }

  deleteJob(id: string) {
    return this.delete('/jobs', { params: { id } })
  }

  runJob(id: string) {
    return this.post<{ ok: boolean; output: string }>('/jobs/run', null, { params: { id } })
  }

  workers(app: string) {
    return this.get<Worker[]>('/workers', { params: { app } })
  }

  setWorker(app: string, name: string, command: string, replicas: number) {
    return this.post<Worker>('/workers', { name, command, replicas }, { params: { app } })
  }

  deleteWorker(id: string) {
    return this.delete('/workers', { params: { id } })
  }

  volumes(app: string) {
    return this.get<Volumes>('/volumes', { params: { app } })
  }

  addVolume(app: string, name: string, path: string) {
    return this.post('/volumes', { name, path }, { params: { app } })
  }

  deleteVolume(app: string, name: string) {
    return this.delete('/volumes', { params: { app, name } })
  }
}

export const projectsService = new ProjectsService()
