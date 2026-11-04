// Shared display formatters — one source of truth so relative times and byte
// sizes read the same everywhere (they had drifted across ~9 copies).

export function relTime(unix: number): string {
  const s = Math.max(0, Math.floor(Date.now() / 1000 - unix))
  if (s < 60) return 'just now'
  if (s < 3600) return Math.floor(s / 60) + 'm ago'
  if (s < 86400) return Math.floor(s / 3600) + 'h ago'
  return Math.floor(s / 86400) + 'd ago'
}

export function fmtDuration(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds))
  if (s < 60) return s + 's'
  if (s < 3600) return Math.floor(s / 60) + 'm ' + (s % 60) + 's'
  return Math.floor(s / 3600) + 'h ' + Math.floor((s % 3600) / 60) + 'm'
}

export function fmtBytes(b: number): string {
  if (b >= 1 << 30) return (b / (1 << 30)).toFixed(1) + 'GB'
  if (b >= 1 << 20) return (b / (1 << 20)).toFixed(1) + 'MB'
  if (b >= 1 << 10) return (b / (1 << 10)).toFixed(0) + 'kB'
  return b + 'B'
}

export const fmtMoney = (v: number, currency: string) =>
  new Intl.NumberFormat(undefined, {
    style: 'currency',
    currency,
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(v)
