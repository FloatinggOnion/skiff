import { z } from 'zod'

export const stackSchema = z
  .object({
    source: z.enum(['repo', 'paste']),
    name: z
      .string()
      .trim()
      .min(1, 'A name is required')
      .max(40, 'Keep it under 40 characters')
      .regex(/^[a-z0-9][a-z0-9-]*$/, 'Use lowercase letters, numbers, and hyphens'),
    git: z.string().trim(),
    branch: z.string().trim(),
    path: z.string().trim(),
    token: z.string(),
    compose: z.string(),
  })
  .superRefine((v, ctx) => {
    if (v.source === 'repo' && !/^https?:\/\//.test(v.git)) {
      ctx.addIssue({ code: 'custom', path: ['git'], message: 'Enter an http(s) repository URL' })
    }
    if (v.source === 'paste' && !v.compose.trim()) {
      ctx.addIssue({ code: 'custom', path: ['compose'], message: 'Paste your Compose file' })
    }
  })

export type StackInput = z.infer<typeof stackSchema>
