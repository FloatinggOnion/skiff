import { X } from 'lucide-react'
import { ConsoleTerminal } from '@/components/console-terminal'

export function ConsoleDrawer({ app, onClose }: { app: string; onClose: () => void }) {
  return (
    <div className="animate-drawer-up border-seam dark bg-well text-foreground fixed inset-x-0 bottom-0 z-50 flex h-[60vh] flex-col border-t-2">
      <div className="flex h-10 shrink-0 items-center justify-between gap-3 border-b border-foreground/6 px-4">
        <span className="text-muted-foreground font-mono text-xs">Console · {app}</span>
        <button
          onClick={onClose}
          className="text-muted-foreground hover:text-foreground flex items-center gap-1.5 rounded-md px-2 py-1 font-mono text-[11px] transition-colors hover:bg-foreground/5"
        >
          <X className="h-3.5 w-3.5" />
          close
        </button>
      </div>
      <div className="min-h-0 flex-1 p-3">
        <ConsoleTerminal app={app} />
      </div>
    </div>
  )
}
