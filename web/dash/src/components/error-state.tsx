import { AlertTriangle } from 'lucide-react'

// Shown when a query errors with no data to fall back on. Queries keep retrying
// on their refetch interval, so this clears itself once the request recovers.
export function ErrorState({
  message = 'Something went wrong.',
  className = '',
}: {
  message?: string
  className?: string
}) {
  return (
    <div
      className={
        'text-muted-foreground flex flex-col items-center gap-2 rounded-xl border border-danger/15 bg-danger/[0.03] py-16 text-sm ' +
        className
      }
    >
      <AlertTriangle className="h-6 w-6 text-danger/60" />
      <span>{message}</span>
    </div>
  )
}
