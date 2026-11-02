import { useEffect, useRef } from 'react'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Send } from 'lucide-react'
import { logsService } from '@/services/api.service'
import { queryKeys } from '@/constants/query-keys'
import { errText } from '@/lib/errors'
import { relTime } from '@/lib/format'
import { useConfirm } from '@/providers/confirm-provider'
import { Button } from '@/components/ui/button'
import { logDrainSchema, type LogDrainInput } from '@/validations/log-drain'
import { Section, Field, SaveButton, RevealInput, inputCls } from './ui'

export function LogDrainSection() {
  const qc = useQueryClient()
  const confirm = useConfirm()
  const { data } = useQuery({
    queryKey: queryKeys.logDrain,
    queryFn: () => logsService.drain(),
    refetchInterval: 15000,
  })
  const form = useForm<LogDrainInput>({
    resolver: zodResolver(logDrainSchema),
    defaultValues: { url: '', headerName: '', headerValue: '' },
  })
  const { errors } = form.formState
  const hydrated = useRef(false)
  const reload = () => qc.invalidateQueries({ queryKey: queryKeys.logDrain })

  useEffect(() => {
    if (data && !hydrated.current) {
      form.reset({ url: data.url, headerName: data.headerName, headerValue: '' })
      hydrated.current = true
    }
  }, [data, form])

  const save = useMutation({
    mutationFn: (values: LogDrainInput) => logsService.saveDrain(values),
    onSuccess: () => {
      form.setValue('headerValue', '')
      test.reset()
      reload()
    },
  })
  const test = useMutation({ mutationFn: () => logsService.testDrain() })
  const remove = useMutation({
    mutationFn: () => logsService.removeDrain(),
    onSuccess: () => {
      form.reset({ url: '', headerName: '', headerValue: '' })
      save.reset()
      test.reset()
      reload()
    },
  })

  const on = !!data?.configured
  const error = save.error ?? test.error ?? remove.error
  const dirty = () => {
    save.reset()
    test.reset()
  }

  return (
    <Section
      title="Log drain"
      description="Send every project's logs to a log service as they're written. If the service is unreachable, sending resumes where it stopped."
    >
      <form onSubmit={form.handleSubmit((v) => save.mutate(v))} className="space-y-3">
        <Field label="URL">
          <input
            className={inputCls + ' font-mono'}
            placeholder="https://logs.example.com/ingest"
            aria-invalid={!!errors.url}
            {...form.register('url', { onChange: dirty })}
          />
          {errors.url && <p className="mt-1 text-xs text-danger">{errors.url.message}</p>}
        </Field>
        <Field label="Header name">
          <input
            className={inputCls + ' font-mono'}
            placeholder="Authorization (optional)"
            autoComplete="off"
            aria-invalid={!!errors.headerName}
            {...form.register('headerName', { onChange: dirty })}
          />
          {errors.headerName && (
            <p className="mt-1 text-xs text-danger">{errors.headerName.message}</p>
          )}
        </Field>
        <Field label="Header value">
          <RevealInput
            placeholder={data?.headerSet ? 'Saved — leave blank to keep it' : 'Bearer your-token'}
            autoComplete="new-password"
            {...form.register('headerValue', { onChange: dirty })}
          />
        </Field>
        {error ? (
          <p className="text-xs text-danger">{errText(error, 'Something went wrong.')}</p>
        ) : data?.lastError ? (
          <p className="text-xs text-danger">
            Couldn&apos;t send logs: {data.lastError}. Retrying, and nothing is lost in the
            meantime.
          </p>
        ) : (
          <p className="text-muted-foreground text-xs">
            {!on
              ? 'Lines already saved stay here; a drain sends what apps write from now on.'
              : data?.lastOk
                ? `On — last sent ${relTime(data.lastOk)}.`
                : 'On — nothing sent yet. Lines go out within seconds of an app writing them.'}
          </p>
        )}
        <div className="flex items-center justify-end gap-3">
          {test.isSuccess && (
            <span className="text-muted-foreground font-mono text-xs">Test line accepted ✓</span>
          )}
          {on && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              loading={remove.isPending}
              onClick={async () => {
                if (
                  await confirm({
                    title: 'Turn off the log drain?',
                    description: 'Logs stop going to this service. Lines it already has are kept.',
                    confirmText: 'Turn off',
                  })
                )
                  remove.mutate()
              }}
            >
              Turn off
            </Button>
          )}
          {on && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              loading={test.isPending}
              onClick={() => test.mutate()}
            >
              <Send className="h-4 w-4" />
              Send test
            </Button>
          )}
          <SaveButton busy={save.isPending} saved={save.isSuccess} disabled={false} />
        </div>
      </form>
    </Section>
  )
}
