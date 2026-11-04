import type { Metadata } from "next";
import { Callout } from "@/components/docs-prose";

export const metadata: Metadata = {
  title: "Failure drills",
  description:
    "What happens when an app, database, router, bucket or disk fails on a Skiff server, from drills we run every week, and what we haven't tested.",
  alternates: { canonical: "/docs/failure-drills" },
};

const DRILLS: [string, string, string][] = [
  [
    "An app crashes",
    "Back in about 3 seconds.",
    "Requests fail until it's back.",
  ],
  [
    "An app runs out of memory",
    "Back in about 3 seconds, and you get an alert.",
    "Requests fail until it's back.",
  ],
  [
    "A database process is killed during writes",
    "Writing again within about 4 seconds, with every confirmed write still there.",
    "Writes fail until it's back. Your app has to reconnect.",
  ],
  [
    "The router is killed",
    "Apps keep running, and it serves traffic within a second of restarting.",
    "All public traffic stops until it restarts.",
  ],
  [
    "The dashboard is killed for 30 seconds",
    "Apps keep answering, with no failed requests.",
    "Nothing can deploy, and the dashboard is down, until it restarts.",
  ],
  [
    "The dashboard is killed during a deploy",
    "No failed requests. When it's back, it records how the deploy really ended. If the deploy didn't finish, your current version stays live and you get an alert.",
    "The rest of that deploy's log is lost.",
  ],
  [
    "The dashboard is killed during a backup",
    "Half-written backups are never listed. The partial file is removed later.",
    "That backup is lost, so take another or wait for the next one.",
  ],
  [
    "Docker, every container, the router and the dashboard die at once",
    "With everything started again, the app and database were back within seconds, with every confirmed write still there.",
    "Everything is down until the server is back.",
  ],
  [
    "The offsite bucket goes away",
    "Backups stay on your server, and you get an alert straight away. Point-in-time history alerts after 10 minutes. When the bucket returned, history caught up in about 30 seconds and the failed backup within 5 minutes.",
    "Until it's back, losing the server loses what hasn't been copied.",
  ],
  [
    "The disk fills completely",
    "Apps keep serving and the dashboard keeps answering. Once space returned, the database wrote again within seconds, with every confirmed write still there.",
    "Large database writes, backups and deploys fail until you free space. Your current version stays live.",
  ],
];

export default function FailureDrills() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">Guides</p>
      <h1>Failure drills</h1>
      <p className="text-muted text-lg">
        Every week, an automated test breaks a real Skiff server on purpose and checks what comes
        back. This page lists what it found, and what it doesn&apos;t cover.
      </p>

      <Callout>
        The drills run on a throwaway Linux machine with small test apps and small databases. Your
        times will be longer with more data, and a real server takes time to boot. See the{" "}
        <a className="underline" href="https://github.com/nuelScript/skiff/actions/workflows/drills.yml">latest runs</a> and
        the <a className="underline" href="https://github.com/nuelScript/skiff/blob/main/e2e/drill_test.go">test code</a>.
      </Callout>

      <h2>What we break</h2>
      <div className="not-prose border-line overflow-x-auto rounded-xl border">
        <table className="w-full min-w-[640px] text-left text-sm">
          <thead className="text-subtle">
            <tr className="border-line border-b">
              <th className="px-4 py-2.5 font-medium">What fails</th>
              <th className="px-4 py-2.5 font-medium">What happens</th>
              <th className="px-4 py-2.5 font-medium">What you lose</th>
            </tr>
          </thead>
          <tbody className="text-muted">
            {DRILLS.map(([what, result, cost]) => (
              <tr key={what} className="border-line border-b align-top last:border-0">
                <td className="text-fg px-4 py-2.5 font-medium">{what}</td>
                <td className="px-4 py-2.5">{result}</td>
                <td className="px-4 py-2.5">{cost}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p>
        We also lose a whole server and rebuild it from its bucket, in a separate test. See{" "}
        <a href="/docs/backups">backups</a> for what comes back.
      </p>

      <h2>What the drills changed</h2>
      <p>The first runs found real problems, all fixed before this page went up:</p>
      <ul>
        <li>A deploy interrupted by a dashboard restart stayed marked as building for 15 minutes.</li>
        <li>A backup that failed to reach the bucket sent no alert and waited a day to try again.</li>
        <li>A backup interrupted by a restart left a partial, unencrypted file on disk for good.</li>
        <li>On a full disk, a deploy could end with no record and a log that never finished.</li>
      </ul>

      <h2>What we haven&apos;t tested</h2>
      <ul>
        <li>
          <strong>Real power loss or a reboot.</strong> The closest drill kills every process at once,
          but the disk keeps what it had written, and the test starts the router and dashboard itself
          rather than leaving it to the server.
        </li>
        <li>
          <strong>A failing disk,</strong> as opposed to a full one.
        </li>
        <li>
          <strong>Databases other than Postgres.</strong>{" "}
          MySQL, MongoDB and Redis aren&apos;t in these
          drills.
        </li>
        <li>
          <strong>Large or busy databases.</strong> Every drill uses a small one, and recovery takes
          longer with more data.
        </li>
        <li>
          <strong>Hosted buckets.</strong>{" "}
          The outage drill uses a local bucket that&apos;s switched off,
          not a slow or partly failing provider.
        </li>
        <li>
          <strong>A restart during a restore.</strong> Failed restores are tested, but not a restart
          partway through one.
        </li>
        <li>
          <strong>Long connections during a crash.</strong>{" "}
          WebSockets and public TCP ports aren&apos;t
          in these drills.
        </li>
      </ul>
      <Callout>
        Skiff runs on one server, so if that server is down, your apps are down. Add an{" "}
        <a className="underline" href="/docs/backups">offsite bucket</a> and an alert channel so you can recover and hear
        about it.
      </Callout>
    </article>
  );
}
