import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CloudUpload } from 'lucide-react'
import { databasesService, type OffsiteTarget } from '@/services/api.service'
import { queryKeys } from '@/constants/query-keys'
import { errText } from '@/lib/errors'
import { useConfirm } from '@/providers/confirm-provider'
import { Button } from '@/components/ui/button'
import { Section, Field, SaveButton, RevealInput, inputCls } from './ui'

const empty: OffsiteTarget = {
  endpoint: '',
  region: '',
  bucket: '',
  prefix: 'skiff',
  accessKey: '',
  secretKey: '',
}

export function OffsiteSection({ isOwner }: { isOwner: boolean }) {
  const qc = useQueryClient()
  const confirm = useConfirm()
  const { data } = useQuery({
    queryKey: queryKeys.offsite,
    queryFn: () => databasesService.offsite(),
  })
  const [form, setForm] = useState<OffsiteTarget>(empty)
  const hydrated = useRef(false)

  useEffect(() => {
    if (data && !hydrated.current) {
      setForm(data.bucket ? { ...data, secretKey: '' } : empty)
      hydrated.current = true
    }
  }, [data])

  const enabled = !!data?.bucket
  const set = (k: keyof OffsiteTarget) => (e: { target: { value: string } }) => {
    setForm((f) => ({ ...f, [k]: e.target.value }))
    save.reset()
    test.reset()
  }

  const save = useMutation({
    mutationFn: () => databasesService.saveOffsite(form),
    onSuccess: () => {
      setForm((f) => ({ ...f, secretKey: '' }))
      qc.invalidateQueries({ queryKey: queryKeys.offsite })
    },
  })
  const test = useMutation({ mutationFn: () => databasesService.testOffsite(form) })
  const disable = useMutation({
    mutationFn: () => databasesService.disableOffsite(),
    onSuccess: () => {
      setForm(empty)
      qc.invalidateQueries({ queryKey: queryKeys.offsite })
    },
  })

  const submit = (e: FormEvent) => {
    e.preventDefault()
    test.reset()
    save.mutate()
  }

  const secretHint = data?.hasSecret ? 'Saved — leave blank to keep it' : ''
  const error = save.error ?? test.error ?? disable.error

  return (
    <Section
      title="Offsite backups"
      description="Copy every database backup to an S3-compatible bucket off this server, so a lost box doesn't take the backups with it."
    >
      <form onSubmit={submit} className="space-y-3">
        <Field label="Endpoint">
          <input
            className={inputCls + ' font-mono'}
            placeholder="https://<account>.r2.cloudflarestorage.com"
            value={form.endpoint}
            onChange={set('endpoint')}
            disabled={!isOwner}
          />
        </Field>
        <Field label="Bucket">
          <div className="grid grid-cols-2 gap-2">
            <input
              className={inputCls + ' font-mono'}
              placeholder="bucket"
              value={form.bucket}
              onChange={set('bucket')}
              disabled={!isOwner}
            />
            <input
              className={inputCls + ' font-mono'}
              placeholder="region (optional)"
              value={form.region}
              onChange={set('region')}
              disabled={!isOwner}
            />
          </div>
        </Field>
        <Field label="Folder">
          <input
            className={inputCls + ' font-mono'}
            placeholder="skiff"
            value={form.prefix}
            onChange={set('prefix')}
            disabled={!isOwner}
          />
        </Field>
        <Field label="Access key">
          <input
            className={inputCls + ' font-mono'}
            value={form.accessKey}
            onChange={set('accessKey')}
            disabled={!isOwner}
            autoComplete="off"
          />
        </Field>
        <Field label="Secret key">
          <RevealInput
            placeholder={secretHint}
            value={form.secretKey ?? ''}
            onChange={set('secretKey')}
            disabled={!isOwner}
            autoComplete="new-password"
          />
        </Field>
        {error ? (
          <p className="text-xs text-danger">{errText(error, 'Something went wrong.')}</p>
        ) : (
          <p className="text-muted-foreground text-xs">
            {enabled
              ? `On — backups go to ${data?.bucket}${data?.prefix ? '/' + data.prefix : ''}, keeping the last 30 per database.`
              : 'Works with Cloudflare R2, Backblaze B2, Hetzner Object Storage, AWS S3 and other S3-compatible stores.'}
          </p>
        )}
        {isOwner && (
          <div className="flex items-center justify-end gap-3">
            {test.isSuccess && (
              <span className="text-muted-foreground font-mono text-xs">Bucket is writable ✓</span>
            )}
            {enabled && (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                loading={disable.isPending}
                onClick={async () => {
                  if (
                    await confirm({
                      title: 'Turn off offsite backups?',
                      description:
                        'New backups stay on this server only. Copies already in the bucket are kept.',
                      confirmText: 'Turn off',
                    })
                  )
                    disable.mutate()
                }}
              >
                Turn off
              </Button>
            )}
            <Button
              type="button"
              size="sm"
              variant="outline"
              loading={test.isPending}
              onClick={() => test.mutate()}
            >
              <CloudUpload className="h-4 w-4" />
              Test
            </Button>
            <SaveButton busy={save.isPending} saved={save.isSuccess} disabled={false} />
          </div>
        )}
      </form>
    </Section>
  )
}
