import { useState, type ComponentProps, type ReactNode } from 'react'
import { Check, Eye, EyeOff } from 'lucide-react'
import { Button } from '@/components/ui/button'

export const inputCls =
  'h-9 w-full rounded-[6px] border border-foreground/12 bg-inset px-3 text-sm outline-none placeholder:text-foreground/25 focus-visible:border-foreground/30 disabled:opacity-50'

export function RevealInput(props: ComponentProps<'input'>) {
  const [show, setShow] = useState(false)
  return (
    <div className="relative">
      <input {...props} type={show ? 'text' : 'password'} className={inputCls + ' pr-9'} />
      <button
        type="button"
        tabIndex={-1}
        aria-label={show ? 'Hide password' : 'Show password'}
        onClick={() => setShow((s) => !s)}
        className="text-muted-foreground hover:text-foreground absolute inset-y-0 right-0 flex items-center px-2.5 transition-colors"
      >
        {show ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
      </button>
    </div>
  )
}

export function Section({
  title,
  description,
  children,
}: {
  title: string
  description?: string
  children: ReactNode
}) {
  return (
    <section className="rounded-xl border border-foreground/8 bg-linear-to-b from-foreground/2 to-transparent p-5">
      <div className="mb-4">
        <h2 className="text-sm font-semibold">{title}</h2>
        {description && <p className="text-muted-foreground mt-1 text-xs">{description}</p>}
      </div>
      {children}
    </section>
  )
}

export function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="grid gap-1.5 sm:grid-cols-[7rem_1fr] sm:items-center sm:gap-4">
      <span className="text-muted-foreground text-xs">{label}</span>
      <div>{children}</div>
    </label>
  )
}

export function SaveButton({
  busy,
  saved,
  disabled,
}: {
  busy: boolean
  saved: boolean
  disabled: boolean
}) {
  return (
    <Button type="submit" size="sm" loading={busy} disabled={disabled}>
      {saved ? (
        <>
          <Check className="h-4 w-4" />
          Saved
        </>
      ) : (
        'Save'
      )}
    </Button>
  )
}
