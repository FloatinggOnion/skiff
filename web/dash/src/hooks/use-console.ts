import {
  deployUrl,
  deployLogUrl,
  githubDeployUrl,
  logsUrl,
  redeployUrl,
  rollbackUrl,
  previewUrl,
  openStream,
} from '@/services/stream.service'
import { deploysService } from '@/services/api.service'
import { useCallback, useRef, useState } from 'react'

export type Stream = { title: string; lines: string[]; app?: string }

export function useConsole(onDeployed: () => void) {
  const [stream, setStream] = useState<Stream | null>(null)
  const es = useRef<EventSource | null>(null)
  const stopApp = useRef<string | undefined>(undefined)

  const start = useCallback((title: string, url: string, onDone?: () => void, app?: string) => {
    es.current?.close()
    stopApp.current = app
    setStream({ title, lines: [], app })
    es.current = openStream(url, {
      onLine: (line) => setStream((s) => (s ? { ...s, lines: [...s.lines, line] } : s)),
      onDone: (ok) => {
        setStream((s) => (s ? { ...s, lines: [...s.lines, ok ? '✓ done' : '✗ failed'] } : s))
        onDone?.()
      },
    })
  }, [])

  const close = useCallback(() => {
    es.current?.close()
    es.current = null
    setStream(null)
  }, [])

  const stop = useCallback(() => {
    if (stopApp.current) {
      void deploysService.cancel(stopApp.current, '').catch(() => {})
    }
  }, [])

  const deploy = useCallback(
    (git: string, name: string, port: string, token?: string) =>
      start('Deploying ' + name, deployUrl(git, name, port, token), onDeployed, name),
    [start, onDeployed],
  )

  const deployRepo = useCallback(
    (
      repo: string,
      clone: string,
      branch: string,
      name: string,
      port: string,
      auto: boolean,
      rootDir: string,
    ) =>
      start(
        'Deploying ' + name,
        githubDeployUrl(repo, clone, branch, name, port, auto, rootDir),
        onDeployed,
        name,
      ),
    [start, onDeployed],
  )

  const redeploy = useCallback(
    (name: string) => start('Redeploying ' + name, redeployUrl(name), onDeployed, name),
    [start, onDeployed],
  )

  const rollback = useCallback(
    (app: string, id: string) =>
      start('Rolling back · ' + app, rollbackUrl(app, id), onDeployed, app),
    [start, onDeployed],
  )

  const preview = useCallback(
    (app: string, branch: string) =>
      start('Preview · ' + branch, previewUrl(app, branch), onDeployed),
    [start, onDeployed],
  )

  const follow = useCallback(
    (app: string, id: string) => start('Deploying ' + app, deployLogUrl(app, id), onDeployed, app),
    [start, onDeployed],
  )

  const showLogs = useCallback((name: string) => start('Logs: ' + name, logsUrl(name)), [start])

  const showBuildLog = useCallback(
    (app: string, id: string) => start('Build · ' + app, deployLogUrl(app, id)),
    [start],
  )

  return {
    stream,
    close,
    stop,
    deploy,
    deployRepo,
    follow,
    redeploy,
    rollback,
    preview,
    showLogs,
    showBuildLog,
  }
}
