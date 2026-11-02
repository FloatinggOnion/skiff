import type { Metadata } from "next";
import { Callout, Steps, Step } from "@/components/docs-prose";
import { CodeBlock } from "@/components/code-block";

export const metadata: Metadata = {
  title: "Log drains",
  description:
    "Send every project's logs to a log service over HTTP, with sending that resumes where it stopped after an outage.",
  alternates: { canonical: "/docs/log-drains" },
};

export default function LogDrains() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">
        Guides
      </p>
      <h1>Log drains</h1>
      <p className="text-muted text-lg">
        Skiff keeps seven days of logs for each project. A log drain also sends them to a service of
        your choice, so you can keep them longer, search across projects or alert on them.
      </p>

      <h2>Set one up</h2>
      <Steps>
        <Step n={1} title="Get an ingest URL from your log service">
          Most log services have an HTTP ingest endpoint and a token. Better Stack, Axiom and
          Datadog all do, or use your own endpoint.
        </Step>
        <Step n={2} title="Open Settings → Team → Log drain">
          Team owners can add one drain per team. It covers every project in the team, previews
          included.
        </Step>
        <Step n={3} title="Enter the URL and header">
          The URL must start with <code>https://</code>. If the service wants a token, put it in a
          header, such as <code>Authorization</code> with <code>Bearer your-token</code>, or{" "}
          <code>DD-API-KEY</code> for Datadog. The value is never shown again.
        </Step>
        <Step n={4} title="Send a test line">
          <strong>Send test</strong> posts one line and tells you whether the service accepted it.
        </Step>
      </Steps>

      <h2>What gets sent</h2>
      <p>
        Lines go out in batches of up to 500 as a JSON array, within seconds of an app writing them.
        Each one looks like this:
      </p>
      <CodeBlock label="POST body">{`[
  {
    "timestamp": "2026-11-02T10:15:30.123456789Z",
    "message": "GET /cart 200 12ms",
    "app": "shop",
    "stream": "out",
    "container": "shop-3fa9c2"
  }
]`}</CodeBlock>
      <p>
        <code>stream</code> is <code>out</code> or <code>err</code>. A message longer than 64 KB is
        cut off at that length.
      </p>

      <h2>When the service is down</h2>
      <p>
        Skiff only moves on once the service answers with a 2xx. Until then it keeps retrying, up to
        every five minutes, and the Log drain section shows the error. Sending picks up from the
        same line afterwards, including after a Skiff restart.
      </p>
      <ul>
        <li>
          <strong>Duplicates are possible.</strong> If a response gets lost after the service saved
          a batch, that batch is sent again.
        </li>
        <li>
          <strong>An outage longer than seven days loses lines,</strong> because that&apos;s how long
          Skiff keeps them.
        </li>
        <li>
          <strong>The daily limit applies.</strong> Skiff saves up to 50 MB of logs per project per
          day, and lines past that aren&apos;t saved or sent.
        </li>
      </ul>
      <Callout>
        Your logs leave your server once a drain is on, including anything your apps print, so keep
        secrets out of them. A new drain only sends what apps write from then on, not what&apos;s
        already saved.
      </Callout>

      <h2>Drains on a private network</h2>
      <p>
        By default, drains can only reach public addresses over HTTPS, so a teammate can&apos;t point
        one at something inside your server&apos;s network. If you run your own collector on a private
        address, set <code>SKIFF_ALLOW_PRIVATE_ENDPOINTS=1</code> in the panel&apos;s environment.
        That allows private addresses and plain HTTP for every team on the server, and private
        addresses for offsite backups too.
      </p>
    </article>
  );
}
