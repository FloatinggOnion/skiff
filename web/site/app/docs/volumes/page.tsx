import type { Metadata } from "next";
import { Callout, Steps, Step } from "@/components/docs-prose";
import { CodeBlock } from "@/components/code-block";

export const metadata: Metadata = {
  title: "Persistent volumes",
  description:
    "Keep files your app writes, like a SQLite database or user uploads, across deploys, rollbacks and restarts.",
  alternates: { canonical: "/docs/volumes" },
};

export default function Volumes() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand-2/80 font-mono text-[11px] tracking-[0.3em] uppercase">
        Guides
      </p>
      <h1>Persistent volumes</h1>
      <p className="text-muted text-lg">
        Every deploy starts your app in a fresh container, so anything it wrote to disk is gone. A
        volume is a directory that stays: a SQLite database, user uploads or a cache survive
        deploys, rollbacks, restarts and sleep.
      </p>

      <h2>Add one from the dashboard</h2>
      <Steps>
        <Step n={1} title="Open the project's Settings">
          Under <strong>Volumes</strong>, choose <strong>Add volume</strong>.
        </Step>
        <Step n={2} title="Name it and pick where it mounts">
          For example <code>data</code> at <code>/data</code>. The path is where your app reads and
          writes, so point your database file or upload folder inside it.
        </Step>
        <Step n={3} title="Redeploy">
          The volume is mounted from the next deploy on. Its size shows next to it once it&apos;s in
          use.
        </Step>
      </Steps>

      <h2>Or in skiff.toml</h2>
      <p>
        Apps deployed with <code>skiff deploy</code> list their volumes in <code>skiff.toml</code>,
        up to five per app:
      </p>
      <CodeBlock label="skiff.toml">{`[[volumes]]
name = "data"
path = "/data"

[[volumes]]
name = "uploads"
path = "/app/public/uploads"`}</CodeBlock>

      <h2>Where they&apos;re mounted</h2>
      <ul>
        <li>
          <strong>Every replica</strong> mounts the same volume, including ones added by
          autoscaling. Several replicas writing the same SQLite file at once can lock each other
          out, so keep a SQLite app at one replica.
        </li>
        <li>
          <strong>Workers, jobs and the release command</strong> see the same files, so a migration
          in the release command runs against the live database.
        </li>
        <li>
          <strong>Previews</strong> mount the same paths on empty volumes of their own. A preview
          never reads or changes production&apos;s files, and its volumes are deleted with it.
        </li>
      </ul>

      <h2>Deleting</h2>
      <p>
        Deleting a volume from the dashboard removes its files on the next deploy, once no
        container uses it. Deleting a project deletes its volumes too. <code>skiff down</code>{" "}
        keeps them, so redeploying the same app picks its files back up.
      </p>
      <Callout>
        Volumes live on your server&apos;s disk and aren&apos;t part of database backups. For data
        you can&apos;t lose, use a managed database, which is backed up daily and can be copied to
        an offsite bucket.
      </Callout>
    </article>
  );
}
