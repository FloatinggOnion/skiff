import { useRef, useState, type ChangeEvent } from 'react'
import { Link, useParams, useSearchParams } from 'react-router'
import { useInfiniteQuery, useQueryClient } from '@tanstack/react-query'
import { ChevronRight, Download, File, Folder, Trash2, Upload } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ErrorState } from '@/components/error-state'
import { FeedSkeleton } from '@/components/skeletons'
import { queryKeys } from '@/constants/query-keys'
import { useStorage } from '@/hooks/use-storage'
import { errText } from '@/lib/errors'
import { fmtBytes, relTime } from '@/lib/format'
import { useConfirm } from '@/providers/confirm-provider'
import { storageService } from '@/services/api.service'

const baseName = (path: string) => path.replace(/\/$/, '').split('/').pop() ?? path

export default function BucketBrowserPage() {
  const { id = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const prefix = params.get('prefix') ?? ''
  const { buckets } = useStorage()
  const bucket = buckets.find((b) => b.id === id)
  const qc = useQueryClient()
  const confirm = useConfirm()
  const input = useRef<HTMLInputElement>(null)
  const [uploading, setUploading] = useState<{ name: string; pct: number; left: number } | null>(null)
  const [error, setError] = useState('')

  const listing = useInfiniteQuery({
    queryKey: queryKeys.bucketObjects(id, prefix),
    queryFn: ({ pageParam }) => storageService.objects(id, prefix, pageParam),
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next || undefined,
    retry: false,
  })
  const folders = listing.data?.pages.flatMap((p) => p.folders) ?? []
  const objects = listing.data?.pages.flatMap((p) => p.objects) ?? []
  const reload = () => qc.invalidateQueries({ queryKey: queryKeys.bucketObjects(id, prefix) })
  const open = (next: string) => setParams(next ? { prefix: next } : {})

  const upload = async (e: ChangeEvent<HTMLInputElement>) => {
    const files = [...(e.target.files ?? [])]
    e.target.value = ''
    if (files.length === 0) return
    setError('')
    const taken = files.filter((f) => objects.some((o) => o.key === prefix + f.name))
    if (
      taken.length > 0 &&
      !(await confirm({
        title: taken.length === 1 ? `Replace ${taken[0].name}?` : `Replace ${taken.length} files?`,
        description: 'A file with the same name is already in this folder. It will be overwritten.',
        confirmText: 'Replace',
        destructive: true,
      }))
    )
      return
    try {
      for (const [i, f] of files.entries()) {
        const left = files.length - i - 1
        setUploading({ name: f.name, pct: 0, left })
        await storageService.upload(id, prefix + f.name, f, (sent) =>
          setUploading({ name: f.name, pct: f.size ? Math.round((sent / f.size) * 100) : 100, left }),
        )
      }
    } catch (err) {
      setError(errText(err, 'The upload failed.'))
    } finally {
      setUploading(null)
      reload()
    }
  }

  const remove = async (key: string) => {
    if (
      !(await confirm({
        title: `Delete ${baseName(key)}?`,
        description: 'Apps using this bucket lose it too. This cannot be undone.',
        confirmText: 'Delete',
        destructive: true,
      }))
    )
      return
    setError('')
    try {
      await storageService.removeObject(id, key)
    } catch (err) {
      setError(errText(err, 'Could not delete the file.'))
    }
    reload()
  }

  const crumbs = prefix
    .split('/')
    .filter(Boolean)
    .map((part, i, all) => ({ part, to: all.slice(0, i + 1).join('/') + '/' }))
  const empty = !listing.isPending && !listing.isError && folders.length + objects.length === 0

  return (
    <div className="px-8 py-8">
      <nav className="text-muted-foreground mb-4 flex flex-wrap items-center gap-1.5 text-sm">
        <Link to="/storage" className="hover:text-foreground transition-colors">
          Storage
        </Link>
        <ChevronRight className="h-3.5 w-3.5" />
        <button
          onClick={() => open('')}
          className={crumbs.length ? 'hover:text-foreground' : 'text-foreground font-medium'}
        >
          {bucket?.name ?? 'Bucket'}
        </button>
        {crumbs.map((c, i) => (
          <span key={c.to} className="flex items-center gap-1.5">
            <ChevronRight className="h-3.5 w-3.5" />
            <button
              onClick={() => open(c.to)}
              className={i === crumbs.length - 1 ? 'text-foreground font-medium' : 'hover:text-foreground'}
            >
              {c.part}
            </button>
          </span>
        ))}
      </nav>

      <header className="mb-6 flex flex-wrap items-center justify-between gap-3">
        <h1 className="text-xl font-semibold tracking-tight">
          {crumbs.length ? crumbs[crumbs.length - 1].part : (bucket?.name ?? 'Files')}
        </h1>
        <div>
          <input ref={input} type="file" multiple hidden onChange={upload} />
          <Button size="sm" loading={!!uploading} onClick={() => input.current?.click()}>
            <Upload className="h-4 w-4" />
            Upload files
          </Button>
        </div>
      </header>

      {uploading && (
        <div className="mb-4 rounded-xl border border-foreground/10 p-3">
          <div className="text-muted-foreground mb-2 flex justify-between gap-3 text-xs">
            <span className="truncate">
              Uploading {uploading.name}
              {uploading.left > 0 && `, ${uploading.left} more after this`}
            </span>
            <span className="font-mono tabular-nums">{uploading.pct}%</span>
          </div>
          <div className="h-1 overflow-hidden rounded-full bg-foreground/10">
            <div className="bg-seam h-full transition-[width]" style={{ width: uploading.pct + '%' }} />
          </div>
        </div>
      )}
      {error && <p className="text-danger mb-4 text-sm">{error}</p>}

      {listing.isPending ? (
        <FeedSkeleton rows={4} />
      ) : listing.isError ? (
        <ErrorState message={errText(listing.error, "Couldn't read this bucket.")} />
      ) : empty ? (
        <div className="text-muted-foreground flex flex-col items-center gap-3 rounded-xl border border-foreground/8 py-16 text-sm">
          <span>{prefix ? 'This folder is empty.' : 'No files in this bucket yet.'}</span>
          <Button size="sm" variant="outline" onClick={() => input.current?.click()}>
            Upload files
          </Button>
        </div>
      ) : (
        <div className="divide-y divide-foreground/6 overflow-hidden rounded-xl border border-foreground/8">
          {folders.map((f) => (
            <button
              key={f}
              onClick={() => open(f)}
              className="flex w-full items-center gap-3 px-4 py-2.5 text-left text-sm transition-colors hover:bg-foreground/4"
            >
              <Folder className="text-seam h-4 w-4 shrink-0" />
              <span className="min-w-0 flex-1 truncate font-medium">{baseName(f)}</span>
              <ChevronRight className="text-muted-foreground h-3.5 w-3.5 shrink-0" />
            </button>
          ))}
          {objects.map((o) => (
            <div key={o.key} className="group flex items-center gap-3 px-4 py-2.5 text-sm">
              <File className="text-muted-foreground h-4 w-4 shrink-0" />
              <span className="min-w-0 flex-1 truncate">{baseName(o.key)}</span>
              <span className="text-muted-foreground hidden shrink-0 font-mono text-xs tabular-nums sm:block">
                {fmtBytes(o.size)}
              </span>
              <span className="text-muted-foreground hidden w-20 shrink-0 text-right font-mono text-[11px] sm:block">
                {relTime(o.modified)}
              </span>
              <a
                href={storageService.objectUrl(id, o.key)}
                title={'Download ' + baseName(o.key)}
                aria-label={'Download ' + baseName(o.key)}
                className="text-muted-foreground hover:text-foreground shrink-0 p-1"
              >
                <Download className="h-3.5 w-3.5" />
              </a>
              <button
                onClick={() => remove(o.key)}
                title={'Delete ' + baseName(o.key)}
                aria-label={'Delete ' + baseName(o.key)}
                className="text-muted-foreground hover:text-danger shrink-0 p-1"
              >
                <Trash2 className="h-3.5 w-3.5" />
              </button>
            </div>
          ))}
        </div>
      )}
      {listing.hasNextPage && (
        <div className="mt-4 flex justify-center">
          <Button
            size="sm"
            variant="outline"
            loading={listing.isFetchingNextPage}
            onClick={() => listing.fetchNextPage()}
          >
            Load more
          </Button>
        </div>
      )}
    </div>
  )
}
