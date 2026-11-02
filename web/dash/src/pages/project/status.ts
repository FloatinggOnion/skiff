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
