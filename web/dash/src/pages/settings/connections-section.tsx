import { GitBranch } from 'lucide-react'
import { type GithubStatus } from '@/services/api.service'
import { Section } from './ui'

export function ConnectionsSection({ gh }: { gh?: GithubStatus }) {
  const connected = gh?.installed
  return (
    <Section title="Connections" description="Source providers Skiff deploys from.">
      <div className="flex items-center gap-3 rounded-lg border border-white/8 px-3.5 py-3">
        <span className="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-white/10 bg-linear-to-br from-white/[0.07] to-transparent">
          <GitBranch className="h-4 w-4 text-white/70" />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium">GitHub</p>
          <p className="text-muted-foreground truncate text-xs">
            {connected
              ? 'Connected' + (gh?.slug ? ` · ${gh.slug}` : '')
              : 'Not connected — deploy public repos, or connect for private ones.'}
          </p>
        </div>
        {connected ? (
          <span className="flex shrink-0 items-center gap-1.5 rounded-full border border-emerald-400/20 bg-emerald-400/10 px-2.5 py-1 text-[11px] font-medium text-emerald-300">
            <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
            Connected
          </span>
        ) : (
          <a
            href="/server"
            className="text-muted-foreground hover:text-foreground shrink-0 rounded-[6px] border border-white/12 px-2.5 py-1 text-xs transition hover:border-white/25"
          >
            Set up
          </a>
        )}
      </div>
      {gh?.permissionsUrl && (
        <p className="text-muted-foreground mt-3 text-xs leading-relaxed">
          To post preview links and deploy statuses on pull requests, give the app{' '}
          <span className="text-foreground/80">Pull requests</span> and{' '}
          <span className="text-foreground/80">Commit statuses</span> (Read and write) in its{' '}
          <a href={gh.permissionsUrl} target="_blank" rel="noreferrer" className="underline">
            GitHub permissions
          </a>
          , then accept the update on the installation.
        </p>
      )}
    </Section>
  )
}
