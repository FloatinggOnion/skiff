import { type ReactNode } from 'react'

export function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-muted-foreground mb-1 font-mono text-[10px] tracking-wider uppercase">
        {label}
      </dt>
      <dd className="break-words">{children}</dd>
    </div>
  )
}
