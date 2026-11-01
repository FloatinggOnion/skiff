import { z } from 'zod'

export const previewPasswordSchema = z.object({
  password: z
    .string()
    .min(8, 'Use at least 8 characters')
    .refine((s) => new TextEncoder().encode(s).length <= 72, 'Use at most 72 characters'),
})

export type PreviewPasswordInput = z.infer<typeof previewPasswordSchema>
