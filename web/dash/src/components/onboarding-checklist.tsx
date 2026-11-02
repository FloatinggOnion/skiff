import { useState } from 'react'
import { useNavigate } from 'react-router'
import { useQuery } from '@tanstack/react-query'
import { Check, X, ChevronRight, Rocket, Globe, KeyRound } from 'lucide-react'
import type { ComponentType } from 'react'
import { envService } from '@/services/api.service'
import { useDomains } from '@/hooks/use-domains'

const DISMISS_KEY = 'skiff-onboarding-dismissed'

type Step = {
  done: boolean
  label: string
  desc: string
  icon: ComponentType<{ className?: string }>
  action: () => void
}

export function OnboardingChecklist({
  appsCount,
  onDeploy,
}: {
  appsCount: number
  onDeploy: () => void
}) {
  const navigate = useNavigate()
  const { domains } = useDomains()
  const { data: shared = [] } = useQuery({
    queryKey: ['shared-env'],
    queryFn: () => envService.sharedList(),
  })
  const [dismissed, setDismissed] = useState(() => localStorage.getItem(DISMISS_KEY) === '1')

  const steps: Step[] = [
    {
      done: appsCount > 0,
      label: 'Deploy your first project',
      desc: 'Ship an app from a Git repository or a Docker image.',
      icon: Rocket,
      action: onDeploy,
    },
    {
      done: domains.length > 0,
      label: 'Add a custom domain',
      desc: 'Serve an app on your own domain with an automatic certificate.',
      icon: Globe,
      action: () => navigate('/domains'),
    },
    {
      done: shared.length > 0,
      label: 'Set shared environment variables',
      desc: 'Configure values every project inherits.',
      icon: KeyRound,
      action: () => navigate('/env'),
    },
  ]

  const doneCount = steps.filter((s) => s.done).length
  if (dismissed || doneCount === steps.length) return null

  const dismiss = () => {
    localStorage.setItem(DISMISS_KEY, '1')
    setDismissed(true)
  }

  return (
    <section className="animate-rise mb-8 overflow-hidden rounded-xl border border-white/10 bg-linear-to-b from-white/3 to-transparent">
      <div className="flex items-center justify-between border-b border-white/8 px-5 py-3.5">
        <div>
          <h2 className="text-sm font-semibold">Get started with Skiff</h2>
          <p className="text-muted-foreground text-xs">
            {doneCount} of {steps.length} complete
          </p>
        </div>
        <div className="flex items-center gap-3">
          <div className="hidden h-1.5 w-24 overflow-hidden rounded-full bg-white/8 sm:block">
            <div
              className="h-full rounded-full bg-emerald-400 transition-all duration-500"
              style={{ width: (doneCount / steps.length) * 100 + '%' }}
            />
          </div>
          <button
            onClick={dismiss}
            aria-label="Dismiss"
            className="text-muted-foreground hover:text-foreground p-1"
          >
            <X className="h-4 w-4" />
          </button>
        </div>
      </div>

      <div className="divide-y divide-white/5">
        {steps.map((s) => (
          <button
            key={s.label}
            onClick={s.done ? undefined : s.action}
            disabled={s.done}
            className={
              'group flex w-full items-center gap-3.5 px-5 py-3 text-left transition-colors ' +
              (s.done ? 'cursor-default' : 'hover:bg-white/2')
            }
          >
            <span
              className={
                'grid h-6 w-6 shrink-0 place-items-center rounded-full border ' +
                (s.done
                  ? 'border-emerald-400/30 bg-emerald-400/15 text-emerald-300'
                  : 'text-muted-foreground border-white/15')
              }
            >
              {s.done ? <Check className="h-3.5 w-3.5" /> : <s.icon className="h-3.5 w-3.5" />}
            </span>
            <div className="min-w-0 flex-1">
              <p
                className={
                  'text-sm font-medium ' + (s.done ? 'text-muted-foreground line-through' : '')
                }
              >
                {s.label}
              </p>
              <p className="text-muted-foreground truncate text-xs">{s.desc}</p>
            </div>
            {!s.done && (
              <ChevronRight className="text-muted-foreground group-hover:text-foreground h-4 w-4 shrink-0 transition" />
            )}
          </button>
        ))}
      </div>
    </section>
  )
}
