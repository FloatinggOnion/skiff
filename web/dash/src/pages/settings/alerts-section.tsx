import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Bell } from 'lucide-react'
import { alertsService } from '@/services/api.service'
import { useAuthContext } from '@/context/auth-context'
import { queryKeys } from '@/constants/query-keys'
import { errText } from '@/lib/errors'
import { Button } from '@/components/ui/button'
import { Section, Field, SaveButton, inputCls } from './ui'

export function AlertsSection() {
  const qc = useQueryClient()
  const { me } = useAuthContext()
  const owner = me?.role === 'owner'
  const { data } = useQuery({ queryKey: queryKeys.alerts, queryFn: () => alertsService.config() })
  const [email, setEmail] = useState('')
  const [slackUrl, setSlackUrl] = useState('')
  const [webhookUrl, setWebhookUrl] = useState('')
  const hydrated = useRef(false)

  useEffect(() => {
    if (data && !hydrated.current) {
      setEmail(data.email)
      setSlackUrl(data.slackUrl)
      setWebhookUrl(data.webhookUrl)
      hydrated.current = true
    }
  }, [data])

  const dirty =
    !!data &&
    (email.trim() !== data.email ||
      slackUrl.trim() !== data.slackUrl ||
      webhookUrl.trim() !== data.webhookUrl)
  const hasChannels = !!data && !!(data.email || data.slackUrl || data.webhookUrl)

  const save = useMutation({
    mutationFn: () =>
      alertsService.save({
        email: email.trim(),
        slackUrl: slackUrl.trim(),
        webhookUrl: webhookUrl.trim(),
      }),
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.alerts }),
  })

  const test = useMutation({ mutationFn: () => alertsService.test() })
  const testMsg = test.isError
    ? 'Test failed.'
    : test.data
      ? test.data.results.length === 0
        ? 'No channels configured.'
        : test.data.results.map((r) => `${r.channel} ${r.ok ? '✓' : '✗'}`).join('  ·  ')
      : ''

  const submit = (e: FormEvent) => {
    e.preventDefault()
    test.reset()
    save.mutate()
  }

  return (
    <Section
      title="Alerts"
      description="Get notified when a deploy fails, an app goes down or runs out of memory, or 5xx errors spike."
    >
      <form onSubmit={submit} className="space-y-3">
        {!owner && (
          <p className="text-muted-foreground text-xs">
            Only team owners can change where alerts go.
          </p>
        )}
        <Field label="Email">
          <input
            className={inputCls}
            disabled={!owner}
            type="email"
            placeholder="you@example.com"
            value={email}
            onChange={(e) => {
              setEmail(e.target.value)
              save.reset()
            }}
          />
        </Field>
        <Field label="Slack">
          <input
            className={inputCls}
            disabled={!owner}
            placeholder="https://hooks.slack.com/services/…"
            value={slackUrl}
            onChange={(e) => {
              setSlackUrl(e.target.value)
              save.reset()
            }}
          />
        </Field>
        <Field label="Webhook">
          <input
            className={inputCls}
            disabled={!owner}
            placeholder="https://your-service/hook"
            value={webhookUrl}
            onChange={(e) => {
              setWebhookUrl(e.target.value)
              save.reset()
            }}
          />
        </Field>
        {email.trim() !== '' && data && !data.smtp && (
          <p className="text-muted-foreground text-xs sm:pl-32">
            Email needs SMTP configured on the server (
            <span className="font-mono">SKIFF_SMTP_*</span>).
          </p>
        )}
        {save.isError && (
          <p className="text-xs text-danger">{errText(save.error, 'Could not save alerts.')}</p>
        )}
        <div className="flex items-center justify-end gap-3">
          {testMsg && <span className="text-muted-foreground font-mono text-xs">{testMsg}</span>}
          <Button
            type="button"
            size="sm"
            variant="outline"
            loading={test.isPending}
            onClick={() => test.mutate()}
            disabled={!owner || !hasChannels || dirty}
          >
            <Bell className="h-4 w-4" />
            Send test
          </Button>
          <SaveButton busy={save.isPending} saved={save.isSuccess} disabled={!owner || !dirty} />
        </div>
      </form>
    </Section>
  )
}
