import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { queryKeys } from '@/constants/query-keys'
import { errText } from '@/lib/errors'
import { useConfirm } from '@/providers/confirm-provider'
import { projectsService, type PreviewDataMode } from '@/services/api.service'

const options: { mode: PreviewDataMode; title: string; about: string }[] = [
  { mode: '', title: "Share the project's settings", about: 'Previews connect to whatever their environment variables say.' },
  { mode: 'empty', title: 'An empty database', about: 'Each preview starts with a new, empty database of its own.' },
  { mode: 'copy', title: 'A copy of the latest backup', about: 'Each preview starts with its own copy of real data, up to a day old.' },
]

export function PreviewData({ app }: { app: string }) {
  const qc = useQueryClient()
  const confirm = useConfirm()
  const [error, setError] = useState('')
  const key = queryKeys.previewData(app)
  const { data } = useQuery({ queryKey: key, queryFn: () => projectsService.previewData(app) })

  const choose = async (mode: PreviewDataMode) => {
    if (mode === data?.mode) return
    if (
      mode === 'copy' &&
      !(await confirm({
        title: 'Copy real data into previews?',
        description:
          'Anyone who can open a preview can reach what its app shows. Set a preview password, and do not use this for data your previews should not show.',
        confirmText: 'Copy data into previews',
        destructive: true,
      }))
    )
      return
    setError('')
    try {
      await projectsService.setPreviewData(app, mode)
      qc.invalidateQueries({ queryKey: key })
    } catch (err) {
      setError(errText(err, 'Could not change this.'))
    }
  }

  return (
    <section className="rounded-xl border border-foreground/8 p-4">
      <h2 className="text-sm font-medium">Preview databases</h2>
      <p className="text-muted-foreground mt-1 mb-3 text-xs">
        {data?.databases.length
          ? `What a new preview gets for ${data.databases.join(', ')}. Previews that already exist keep what they have.`
          : 'This project has no attached databases yet, so this applies once you attach one.'}
      </p>
      <div className="flex flex-col gap-2" role="radiogroup" aria-label="Preview databases">
        {options.map((o) => (
          <label
            key={o.mode || 'share'}
            className="hover:bg-foreground/3 flex cursor-pointer items-start gap-2.5 rounded-lg border border-foreground/8 p-3 transition"
          >
            <input
              type="radio"
              name="preview-data"
              className="mt-0.5"
              checked={(data?.mode ?? '') === o.mode}
              onChange={() => void choose(o.mode)}
            />
            <span>
              <span className="block text-sm">{o.title}</span>
              <span className="text-muted-foreground block text-xs">{o.about}</span>
            </span>
          </label>
        ))}
      </div>
      {error && <p className="mt-2 text-xs text-danger">{error}</p>}
    </section>
  )
}
