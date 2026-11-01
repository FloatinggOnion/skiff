import { z } from 'zod'

export const projectSettingsSchema = z
  .object({
    branch: z.string().trim(),
    rootDir: z.string().trim(),
    port: z
      .string()
      .trim()
      .refine((v) => v === '' || /^\d+$/.test(v), 'Port must be a number'),
    auto: z.boolean(),
    previewAuto: z.boolean(),
    replicas: z.number().int().min(1).max(10),
    release: z.string().trim(),
    autoscale: z.boolean(),
    scaleMin: z.number().int().min(1).max(10),
    scaleMax: z.number().int().min(1).max(10),
    scaleCpu: z.number().int().min(10, 'Target must be 10–100%').max(100, 'Target must be 10–100%'),
    sleepAfter: z.number().int().min(0),
    healthPath: z
      .string()
      .trim()
      .refine((v) => v === '' || v.startsWith('/'), 'Start with /, like /healthz')
      .refine((v) => !/[\s\x00-\x1f\x7f]/.test(v), "Can't contain spaces"),
    healthTimeout: z
      .number({ error: 'Enter a number of seconds' })
      .int()
      .refine((v) => v === 0 || (v >= 5 && v <= 600), '5–600 seconds, or 0 for the default'),
  })
  .refine((v) => !v.autoscale || v.scaleMax >= v.scaleMin, {
    message: 'Max replicas must be at least the minimum',
    path: ['scaleMax'],
  })

export type ProjectSettingsInput = z.infer<typeof projectSettingsSchema>
