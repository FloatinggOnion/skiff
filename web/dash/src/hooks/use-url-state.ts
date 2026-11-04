import { useCallback } from 'react'
import { useSearchParams } from 'react-router'

// A value kept in the page address, so a reload or a shared link keeps it. The default stays out of the address.
export function useUrlState(key: string, fallback = '') {
  const [params, setParams] = useSearchParams()
  const value = params.get(key) ?? fallback
  const set = useCallback(
    (next: string) => {
      const wanted = next || fallback
      if (wanted === (params.get(key) ?? fallback)) return
      const out = new URLSearchParams(params)
      if (wanted === fallback) out.delete(key)
      else out.set(key, wanted)
      setParams(out, { replace: true })
    },
    [params, setParams, key, fallback],
  )
  return [value, set] as const
}
