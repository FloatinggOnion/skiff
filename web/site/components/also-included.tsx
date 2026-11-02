const ITEMS = [
  ["Prebuilt images", "Run any image from Docker Hub or a private registry."],
  ["Persistent volumes", "Files that survive deploys, for SQLite and uploads."],
  ["Object storage", "S3-compatible buckets with a file browser."],
  ["Workers and cron", "Background processes and scheduled jobs."],
  ["Autoscaling", "Add and retire replicas to hold a target CPU."],
  ["Sleep when idle", "Stop quiet apps; the next visit wakes them."],
  ["Log drains", "Send logs to Better Stack, Axiom, Datadog or your own endpoint."],
  ["Alerts", "Email, Slack or webhook when something goes wrong."],
  ["Email forwarding", "hello@yourdomain.com to any inbox."],
  ["Offsite backups", "Database backups copied to any S3-compatible bucket."],
  ["Teams and API tokens", "Collaborators, an audit log and an API for CI."],
  ["Two-factor sign-in", "Authenticator codes with recovery codes."],
];

export function AlsoIncluded() {
  return (
    <section className="mx-auto max-w-6xl px-6 pb-24">
      <h2 className="font-display text-3xl font-bold tracking-tight sm:text-4xl">Also in the boat.</h2>
      <dl className="border-line mt-10 grid gap-x-10 border-t sm:grid-cols-2 lg:grid-cols-3">
        {ITEMS.map(([k, v]) => (
          <div key={k} className="border-line border-b py-4">
            <dt className="font-medium">{k}</dt>
            <dd className="text-muted mt-1 text-sm leading-relaxed">{v}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}
