import { Input } from '@/components/ui/input'
import type { EnvVar } from '@/services/api.service'
import { Plus, X } from 'lucide-react'
import { type ClipboardEvent } from 'react'

export const blankVar = (): EnvVar => ({ key: '', value: '', build: false })

// Parse pasted .env content: KEY=value lines, tolerating `export`, quotes,
// `#` comments, and blank lines.
export function parseEnv(text: string): EnvVar[] {
  const out: EnvVar[] = []
  for (const raw of text.split(/\r?\n/)) {
    let line = raw.trim()
    if (!line || line.startsWith('#')) continue
    if (line.startsWith('export ')) line = line.slice(7).trimStart()
    const eq = line.indexOf('=')
    if (eq < 1) continue
    const key = line.slice(0, eq).trim()
    let value = line.slice(eq + 1).trim()
    const quote = value[0]
    if ((quote === '"' || quote === "'") && value.endsWith(quote) && value.length > 1) {
      value = value.slice(1, -1)
    }
    if (/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) out.push({ key, value, build: false })
  }
  return out
}

function merge(existing: EnvVar[], pasted: EnvVar[]): EnvVar[] {
  const map = new Map<string, EnvVar>()
  for (const v of existing) if (v.key.trim()) map.set(v.key, v)
  for (const p of pasted) map.set(p.key, p)
  const out = Array.from(map.values())
  return out.length ? out : [blankVar()]
}

// A controlled editor for a list of env vars. Pasting a .env into any name
// field fills the rows. The parent owns state + persistence.
export function EnvFields({
  vars,
  onChange,
}: {
  vars: EnvVar[]
  onChange: (vars: EnvVar[]) => void
}) {
  const update = (i: number, patch: Partial<EnvVar>) =>
    onChange(vars.map((v, idx) => (idx === i ? { ...v, ...patch } : v)))

  const onPaste = (e: ClipboardEvent<HTMLInputElement>) => {
    const text = e.clipboardData.getData('text')
    if (!/[\n=]/.test(text)) return
    const parsed = parseEnv(text)
    if (parsed.length === 0) return
    e.preventDefault()
    onChange(merge(vars, parsed))
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="divide-y divide-foreground/5 overflow-hidden rounded-md border border-foreground/8">
        <div className="text-muted-foreground grid grid-cols-[1fr_1.5fr_auto] gap-3 px-3 py-2 font-mono text-[10px] tracking-wider uppercase">
          <span>Name</span>
          <span>Value</span>
          <span className="w-[92px]" />
        </div>
        {vars.map((v, i) => (
          <div
            key={i}
            className="grid grid-cols-[1fr_1.5fr_auto] items-center gap-3 px-3 py-1.5 focus-within:bg-foreground/2"
          >
            <Input
              placeholder="EXAMPLE_KEY"
              value={v.key}
              onChange={(e) => update(i, { key: e.target.value })}
              onPaste={onPaste}
              className="h-8 border-0 bg-transparent px-0 font-mono text-sm shadow-none focus-visible:ring-0"
            />
            <Input
              placeholder="value"
              value={v.value}
              onChange={(e) => update(i, { value: e.target.value })}
              className="h-8 border-0 bg-transparent px-0 font-mono text-sm shadow-none focus-visible:ring-0"
            />
            <div className="flex w-[92px] items-center justify-end gap-1">
              <button
                type="button"
                title="Available at build time"
                onClick={() => update(i, { build: !v.build })}
                className={
                  'rounded-[5px] border px-2 py-1 font-mono text-[10px] tracking-wide uppercase transition-colors ' +
                  (v.build
                    ? 'border-success/30 bg-success/10 text-success'
                    : 'text-muted-foreground hover:text-foreground border-foreground/12')
                }
              >
                build
              </button>
              <button
                type="button"
                title="Remove"
                onClick={() =>
                  onChange(vars.length > 1 ? vars.filter((_, idx) => idx !== i) : [blankVar()])
                }
                className="text-muted-foreground hover:text-foreground rounded-[5px] p-1.5"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>
        ))}
      </div>
      <button
        type="button"
        onClick={() => onChange([...vars, blankVar()])}
        className="text-muted-foreground hover:text-foreground flex w-fit items-center gap-1.5 text-sm transition-colors"
      >
        <Plus className="h-3.5 w-3.5" /> Add variable
      </button>
    </div>
  )
}
