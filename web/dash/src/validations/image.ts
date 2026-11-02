import { z } from 'zod'

export const imageRef = z
  .string()
  .trim()
  .min(1, 'An image is required')
  .max(255, 'Keep it under 255 characters')
  .regex(/^[A-Za-z0-9._/:@-]+$/, 'Use a reference like nginx:1.27 or ghcr.io/you/app:latest')
  .refine((v) => !v.startsWith('-'), 'Use a reference like nginx:1.27')

export const optionalPort = z
  .string()
  .trim()
  .refine(
    (v) => v === '' || (/^\d+$/.test(v) && Number(v) >= 1 && Number(v) <= 65535),
    'Use a port from 1 to 65535, or leave it empty',
  )

export const imageProjectSchema = z
  .object({
    name: z
      .string()
      .trim()
      .min(1, 'A name is required')
      .regex(/^[a-z0-9][a-z0-9-]*$/, 'Use lowercase letters, numbers, and hyphens'),
    image: imageRef,
    port: optionalPort,
    username: z.string().trim(),
    password: z.string(),
  })
  .refine((v) => !v.username || v.password, {
    message: 'Enter the password or access token',
    path: ['password'],
  })

export type ImageProjectInput = z.infer<typeof imageProjectSchema>

export const registrySchema = z.object({
  username: z.string().trim().min(1, 'A username is required'),
  password: z.string().min(1, 'Enter the password or access token'),
})

export type RegistryInput = z.infer<typeof registrySchema>
