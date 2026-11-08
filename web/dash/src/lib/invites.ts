export type InviteSent = { to: string; emailed?: boolean }

export function inviteNote({ to, emailed }: InviteSent) {
  if (emailed) return `Invite emailed to ${to}. You can also share the link yourself:`
  if (emailed === false) return `We couldn't email the invite, so share this link with ${to}:`
  return `Share this link with ${to}. It works once and expires in 7 days.`
}
