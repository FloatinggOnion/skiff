import { Check, Copy } from 'lucide-react'
import { useCopy } from '@/hooks/use-copy'

export function Copyable({ text }: { text: string }) {
  const { copied, copy } = useCopy()
  return (
    <button
      type="button"
      onClick={() => copy(text)}
      className="group/copy hover:text-foreground text-foreground/90 flex min-w-0 items-center gap-1.5 truncate text-left transition"
      title="Copy"
    >
      <span className="truncate">{text}</span>
      {copied ? (
        <Check className="h-3 w-3 shrink-0 text-success" />
      ) : (
        <Copy className="h-3 w-3 shrink-0 opacity-0 transition group-hover/copy:opacity-60" />
      )}
    </button>
  )
}
