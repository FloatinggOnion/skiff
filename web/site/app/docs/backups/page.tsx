import type { Metadata } from "next";
import { Callout, Steps, Step } from "@/components/docs-prose";

export const metadata: Metadata = {
  title: "Backups and encryption",
  description:
    "What Skiff backs up, how restoring works, what's encrypted on your server, and how to keep your recovery key safe.",
  alternates: { canonical: "/docs/backups" },
};

export default function Backups() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">Guides</p>
      <h1>Backups and encryption</h1>
      <p className="text-muted text-lg">
        Skiff backs up your databases and volumes every day and encrypts the secrets it stores. This
        page covers what that protects, what it doesn&apos;t, and the two things to set up yourself.
      </p>

      <Callout>
        Backups start on the same server as your apps, so a dead server takes them with it until you
        add an offsite bucket. Daily backups also mean you can lose up to 24 hours of data.
      </Callout>

      <h2>What gets backed up</h2>
      <ul>
        <li>
          <strong>Databases:</strong> Postgres, MySQL, MongoDB and Redis, daily. Skiff keeps the last
          10 on your server.
        </li>
        <li>
          <strong>Volumes:</strong> daily, keeping the last 3 on your server. Previews aren&apos;t
          backed up, because they use empty copies of their project&apos;s volumes.
        </li>
        <li>
          <strong>Skiff itself:</strong> its database and settings, sealed with your recovery key.
          See <a href="/docs/self-hosting">self-hosting</a>.
        </li>
      </ul>
      <p>
        You can also take a backup at any time. For a database, open <strong>Backups</strong> on it.
        For a volume, open the project&apos;s Settings and use the backups button beside the volume.
      </p>
      <p>
        Your app pauses briefly while a volume is copied, so files like SQLite databases stay
        consistent. Requests wait during the pause. Volumes over 1 GB are copied while the app runs,
        because the pause would be too long, and are labelled <em>copied while running</em>. If your
        app writes constantly, check that those copies restore cleanly. A backup is skipped when it
        could fill the disk.
      </p>

      <h2>Add an offsite bucket</h2>
      <Steps>
        <Step n={1} title="Create a private bucket">
          Any S3-compatible storage works. Keep it private, because database and volume backups are
          stored there unencrypted.
        </Step>
        <Step n={2} title="Add it in Settings">
          Enter the endpoint, bucket and keys in the offsite section. New backups are copied there,
          and the bucket keeps up to 30 per database or volume.
        </Step>
      </Steps>

      <h2>Restoring</h2>
      <p>
        A restore returns the database or volume to exactly what&apos;s in the backup. Anything
        created after it is removed, including new tables and files. Before every restore, Skiff
        backs up the current data, so you can undo it. That backup is listed as{" "}
        <em>before restore</em>. If it can&apos;t be taken, the restore doesn&apos;t run.
      </p>
      <ul>
        <li>
          <strong>Postgres</strong> restores in one transaction, so a failure changes nothing. Other
          connections are closed first, so your apps will reconnect.
        </li>
        <li>
          <strong>MySQL and MongoDB</strong> can&apos;t do this in one step, so a failure partway can
          leave them incomplete. If that happens, restore the <em>before restore</em> backup.
        </li>
        <li>
          <strong>Redis</strong> stops briefly. The file is checked first, so a damaged backup causes
          no downtime.
        </li>
        <li>
          <strong>Volumes</strong> unpack while your app keeps running, and it restarts for a moment
          when the files are swapped. If the swap fails, the original files go back. If the backup is
          no longer on your server, Skiff fetches it from your bucket.
        </li>
      </ul>
      <p>
        After moving to a new server, managed databases load their newest offsite backup on their
        own. Volumes don&apos;t: redeploy the app, then restore the volume from its backups.
      </p>

      <p>
        Deleting a database or volume also deletes its backups from your server. Copies in your bucket
        stay, so without a bucket a delete is final.
      </p>

      <h2>What&apos;s encrypted</h2>
      <p>These are encrypted in Skiff&apos;s database with a key derived from your recovery key:</p>
      <ul>
        <li>Environment variables, including shared and preview ones</li>
        <li>Database passwords, and bucket and offsite keys</li>
        <li>Registry passwords</li>
        <li>Alert URLs, and log drain URLs and headers</li>
        <li>Two-factor secrets</li>
      </ul>
      <p>
        Login sessions and invite links are stored as hashes, so they can&apos;t be read back.
        Passwords and API tokens are hashed too.
      </p>

      <h3>What this protects against</h3>
      <p>
        It protects a copy of the database file on its own, such as one left in a stray backup or on
        an old disk. It doesn&apos;t protect against someone with root on your server, or anyone who
        copies all of <code>~/.skiff</code>, because the key is kept there too. Your apps also receive
        their secrets as ordinary environment variables while they run.
      </p>
      <p>
        These aren&apos;t encrypted: database and volume backups, the GitHub and email settings files
        in <code>~/.skiff</code>, and any data your apps store themselves.
      </p>

      <h2>Save your recovery key</h2>
      <p>
        Find it in the dashboard under <strong>Server</strong>, in the control-plane backup section,
        and keep it somewhere off the server, such as a password manager.
      </p>
      <Callout>
        Without this key, stored secrets can&apos;t be recovered. If <code>recovery.key</code> goes
        missing or doesn&apos;t match, the panel refuses to start rather than create a new one. Put
        the original file back to fix it.
      </Callout>

      <h2>Downgrading</h2>
      <p>
        Older versions can&apos;t read encrypted secrets, so going back to a version before v0.6.0 will
        break them. When the upgrade converts your secrets, Skiff first writes a sealed copy of the
        old database to <code>~/.skiff/pre-encryption-*.skiffbackup</code> and keeps it for 14 days.{" "}
        <code>skiff server restore</code> can read it.
      </p>
    </article>
  );
}
