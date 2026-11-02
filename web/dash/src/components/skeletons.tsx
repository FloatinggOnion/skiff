import { Skeleton } from '@/components/ui/skeleton'

// Loading placeholders that mirror the real components' silhouettes, so the swap
// from skeleton to content doesn't shift the layout.

// One placeholder project card — matches AppCard's shape.
function AppCardSkeleton() {
  return (
    <div className="flex flex-col gap-3.5 rounded-xl border border-foreground/8 bg-foreground/1.5 p-4">
      <div className="flex items-center gap-2.5">
        <Skeleton className="h-8 w-8 shrink-0 rounded-md" />
        <div className="flex-1 space-y-2">
          <Skeleton className="h-3.5 w-24" />
          <Skeleton className="h-3 w-32" />
        </div>
        <Skeleton className="h-5 w-5 shrink-0 rounded-full" />
      </div>
      <Skeleton className="h-6 w-40 rounded-md" />
      <div className="space-y-2 border-t border-foreground/5 pt-3.5">
        <Skeleton className="h-3.5 w-full" />
        <Skeleton className="h-3.5 w-2/3" />
      </div>
      <Skeleton className="h-3 w-20" />
    </div>
  )
}

// A grid of placeholder cards, drop into the same grid container as AppCards.
export function CardGridSkeleton({ count = 6 }: { count?: number }) {
  return (
    <>
      {Array.from({ length: count }).map((_, i) => (
        <AppCardSkeleton key={i} />
      ))}
    </>
  )
}

// A vertical stack of card-height placeholders — for database / bucket lists.
export function CardListSkeleton({ count = 3 }: { count?: number }) {
  return (
    <div className="space-y-3">
      {Array.from({ length: count }).map((_, i) => (
        <div
          key={i}
          className="flex items-center gap-3 rounded-xl border border-foreground/8 bg-foreground/1.5 p-4"
        >
          <Skeleton className="h-9 w-9 shrink-0 rounded-md" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-3.5 w-40" />
            <Skeleton className="h-3 w-56 max-w-[70%]" />
          </div>
          <Skeleton className="h-7 w-16 shrink-0 rounded-md" />
        </div>
      ))}
    </div>
  )
}

// Full-page placeholder for the Analytics route while its (lazy) recharts chunk
// downloads — mirrors the page so the chunk-load → data-load transition is
// seamless instead of a "Loading…" flash.
export function AnalyticsSkeleton() {
  return (
    <div className="px-8 py-8">
      <div className="mb-6 space-y-2">
        <Skeleton className="h-6 w-28" />
        <Skeleton className="h-3.5 w-72 max-w-[70%]" />
      </div>
      <Skeleton className="mb-4 h-3 w-24" />
      <ChartGridSkeleton count={4} />
      <div className="mt-10">
        <Skeleton className="mb-4 h-3 w-20" />
        <ChartGridSkeleton count={4} />
      </div>
    </div>
  )
}

// A grid of chart-card placeholders — for the analytics panels.
export function ChartGridSkeleton({ count = 4 }: { count?: number }) {
  return (
    <div className="grid gap-4 lg:grid-cols-2">
      {Array.from({ length: count }).map((_, i) => (
        <div key={i} className="rounded-xl border border-foreground/8 bg-foreground/1.5 p-4">
          <Skeleton className="mb-4 h-3.5 w-32" />
          <Skeleton className="h-40 w-full" />
        </div>
      ))}
    </div>
  )
}

// A bordered list of shimmer rows — for activity / deployment / database feeds.
export function FeedSkeleton({ rows = 6 }: { rows?: number }) {
  return (
    <div className="overflow-hidden rounded-xl border border-foreground/8">
      {Array.from({ length: rows }).map((_, i) => (
        <div
          key={i}
          className={
            'flex items-center gap-3.5 px-4 py-3.5 ' +
            (i === rows - 1 ? '' : 'border-b border-foreground/6')
          }
        >
          <Skeleton className="h-7 w-7 shrink-0 rounded-full" />
          <div className="flex-1 space-y-2">
            <Skeleton className="h-3.5 w-48 max-w-[60%]" />
            <Skeleton className="h-3 w-32 max-w-[40%]" />
          </div>
          <Skeleton className="h-3 w-12 shrink-0" />
        </div>
      ))}
    </div>
  )
}
