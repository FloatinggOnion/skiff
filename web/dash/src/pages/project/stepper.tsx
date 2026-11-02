export function Stepper({
  value,
  onDec,
  onInc,
  min = 1,
  max = 10,
}: {
  value: number
  onDec: () => void
  onInc: () => void
  min?: number
  max?: number
}) {
  return (
    <div className="flex h-9 w-28 items-center justify-between rounded-md border border-foreground/15 px-1">
      <button
        type="button"
        onClick={onDec}
        disabled={value <= min}
        aria-label="Decrease"
        className="text-muted-foreground hover:text-foreground grid h-7 w-7 place-items-center rounded text-base disabled:opacity-30"
      >
        −
      </button>
      <span className="font-mono text-sm tabular-nums">{value}</span>
      <button
        type="button"
        onClick={onInc}
        disabled={value >= max}
        aria-label="Increase"
        className="text-muted-foreground hover:text-foreground grid h-7 w-7 place-items-center rounded text-base disabled:opacity-30"
      >
        +
      </button>
    </div>
  )
}
