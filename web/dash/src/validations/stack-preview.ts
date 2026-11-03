import { z } from 'zod'

export const stackPreviewBranchSchema = z.object({
  branch: z.string().trim().min(1, 'Enter a branch'),
})

export type StackPreviewBranchInput = z.infer<typeof stackPreviewBranchSchema>
