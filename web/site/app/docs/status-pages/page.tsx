import type { Metadata } from "next";
import { Callout, Steps, Step } from "@/components/docs-prose";

export const metadata: Metadata = {
  title: "Status pages",
  description:
    "Publish a status page with the current state and 30 days of uptime for the projects you choose, on your own domain if you like.",
  alternates: { canonical: "/docs/status-pages" },
};

export default function StatusPages() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">Guides</p>
      <h1>Status pages</h1>
      <p className="text-muted text-lg">
        Give the people who use your apps one place to check whether they&apos;re up. Skiff already
        checks each project every minute, so a status page only needs turning on.
      </p>

      <h2>Publish one</h2>
      <Steps>
        <Step n={1} title="Open Settings → Team → Public status page">
          Only team owners can change it.
        </Step>
        <Step n={2} title="Choose what to show">
          Give it a title, tick the projects to list and turn on <strong>Publish the status page</strong>.
        </Step>
        <Step n={3} title="Share the link">
          It&apos;s at <code>status.yourdomain.com/your-team</code>. If only one team on the server has
          a page, <code>status.yourdomain.com</code> shows it too. The link stays the same if you
          rename the team.
        </Step>
      </Steps>

      <h2>On your own domain</h2>
      <p>
        Enter a domain like <code>status.example.com</code> and point its DNS at your server. The
        certificate is issued automatically. That domain serves only the status page.
      </p>

      <h2>What it shows</h2>
      <ul>
        <li>
          <strong>Current state</strong> of each project you chose. An app that&apos;s asleep counts
          as operational, since the next visit wakes it.
        </li>
        <li>
          <strong>30 days of uptime</strong>, day by day, and the overall percentage.
        </li>
        <li>
          <strong>Recent incidents</strong>, with when they started and how long they lasted.
        </li>
      </ul>
      <Callout>
        Nothing is public until you publish, and only the projects you choose appear. Previews, other
        teams&apos; projects and the error behind an incident are never shown, because those can name
        internal hosts. Project names are shown as they are, so rename anything you wouldn&apos;t want
        public before listing it.
      </Callout>

      <h2>How current it is</h2>
      <p>
        Projects are checked every minute, the page is refreshed at most every 30 seconds and it
        reloads itself every minute. If there are no checks for ten minutes, a project shows{" "}
        <strong>No recent data</strong> rather than claiming it&apos;s up.
      </p>
    </article>
  );
}
