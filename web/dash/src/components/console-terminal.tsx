import { useEffect, useRef } from 'react'
import { Terminal } from '@xterm/xterm'
import { FitAddon } from '@xterm/addon-fit'
import '@xterm/xterm/css/xterm.css'

// A live shell over a WebSocket. Defaults to an app container's console
// (/api/exec); pass `path` to point it elsewhere (e.g. a database shell).
export function ConsoleTerminal({ app, path }: { app?: string; path?: string }) {
  const ref = useRef<HTMLDivElement>(null)
  const wsPath = path ?? `/api/exec?app=${encodeURIComponent(app ?? '')}`

  useEffect(() => {
    const el = ref.current
    if (!el) return

    const term = new Terminal({
      fontFamily: 'ui-monospace, SFMono-Regular, Menlo, monospace',
      fontSize: 13,
      lineHeight: 1.4,
      cursorBlink: true,
      theme: {
        background: '#081513',
        foreground: '#c5d6d1',
        cursor: '#e39155',
        selectionBackground: '#27443f',
        black: '#0e2220',
        brightBlack: '#7d9c94',
      },
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(el)
    try {
      fit.fit()
    } catch {
      /* not laid out yet */
    }

    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const ws = new WebSocket(`${proto}//${location.host}${wsPath}`)
    ws.binaryType = 'arraybuffer'

    ws.onopen = () => {
      term.focus()
      ws.send(JSON.stringify({ t: 'resize', c: term.cols, r: term.rows }))
    }
    ws.onmessage = (e) => {
      if (typeof e.data === 'string') term.write(e.data)
      else term.write(new Uint8Array(e.data as ArrayBuffer))
    }
    ws.onclose = () => term.write('\r\n\x1b[90m— session ended —\x1b[0m\r\n')

    const onData = term.onData((d) => {
      if (ws.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ t: 'in', d }))
    })
    const onResize = term.onResize(({ cols, rows }) => {
      if (ws.readyState === WebSocket.OPEN)
        ws.send(JSON.stringify({ t: 'resize', c: cols, r: rows }))
    })

    const ro = new ResizeObserver(() => {
      try {
        fit.fit()
      } catch {
        /* ignore */
      }
    })
    ro.observe(el)

    return () => {
      ro.disconnect()
      onData.dispose()
      onResize.dispose()
      ws.close()
      term.dispose()
    }
  }, [wsPath])

  return <div ref={ref} className="h-full w-full" />
}
