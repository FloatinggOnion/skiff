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
  })
  .refine((v) => !v.autoscale || v.scaleMax >= v.scaleMin, {
    message: 'Max replicas must be at least the minimum',
    path: ['scaleMax'],
  })

export type ProjectSettingsInput = z.infer<typeof projectSettingsSchema>
