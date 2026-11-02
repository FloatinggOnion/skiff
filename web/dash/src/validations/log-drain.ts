import { z } from 'zod'

export const logDrainSchema = z
  .object({
    url: z
      .string()
      .trim()
      .regex(/^https?:\/\/\S+$/, 'Use the full URL from your log service, starting with https://'),
    headerName: z
      .string()
      .trim()
      .regex(/^[A-Za-z0-9-]*$/, 'Letters, numbers and hyphens only'),
    headerValue: z.string().trim(),
  })
  .refine((v) => v.headerName || !v.headerValue, {
    message: 'Enter the header name too',
    path: ['headerName'],
  })

export type LogDrainInput = z.infer<typeof logDrainSchema>
