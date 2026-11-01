import { z } from 'zod'

const reserved = ['/proc', '/sys', '/dev']

export const volumeSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, 'A name is required')
    .max(32, 'Keep it under 32 characters')
    .regex(/^[a-z0-9][a-z0-9-]*$/, 'Use lowercase letters, numbers, and hyphens'),
  path: z
    .string()
    .trim()
    .regex(/^\/[^:,]+$/, 'Use an absolute path like /data')
    .refine(
      (p) =>
        !p.endsWith('/') && !p.slice(1).split('/').some((s) => s === '' || s === '.' || s === '..'),
      { message: 'Use a plain path like /data' },
    )
    .refine((p) => !reserved.some((r) => p === r || p.startsWith(r + '/')), {
      message: 'That directory belongs to the system',
    }),
})

export type VolumeInput = z.infer<typeof volumeSchema>
