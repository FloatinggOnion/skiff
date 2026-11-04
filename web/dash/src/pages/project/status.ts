// Status → tailwind class helpers shared by the project page and its panels.

export const runningPill = (state: string): string =>
  state === 'running'
    ? 'border-success/25 bg-success/10 text-success'
    : state === 'exited' || state === 'missing'
      ? 'border-danger/25 bg-danger/10 text-danger'
      : 'border-foreground/15 bg-foreground/5 text-muted-foreground'

export const deployDot = (status: string): string =>
  status === 'live'
    ? 'bg-success'
    : status === 'failed'
      ? 'bg-danger'
      : status === 'canceled'
        ? 'bg-foreground/25'
        : 'bg-warning pulse-dot'

export const statusLabel = (status: string): string =>
  status === 'live' ? 'Ready' : status.charAt(0).toUpperCase() + status.slice(1)

const TRIGGERS: Record<string, string> = {
  push: 'Git push',
  manual: 'Dashboard',
  redeploy: 'Redeploy',
  rollback: 'Rollback',
  preview: 'Preview',
  api: 'API',
  cli: 'CLI',
}

export const triggerLabel = (trigger: string): string => TRIGGERS[trigger] ?? trigger

// The part of a deploy id that tells two deploys apart at a glance.
export const shortDeployId = (id: string): string => id.split('-').pop()?.slice(-7) ?? id
