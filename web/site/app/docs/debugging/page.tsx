import type { Metadata } from "next";
import { Callout } from "@/components/docs-prose";
import { CodeBlock } from "@/components/code-block";

export const metadata: Metadata = {
  title: "Debugging",
  description:
    "Find out why a deploy failed, read logs from your app and its workers, open a shell, and reach a database.",
  alternates: { canonical: "/docs/debugging" },
};

export default function Debugging() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">Guides</p>
      <h1>Debugging</h1>
      <p className="text-muted text-lg">
        Where to look when something is wrong: a deploy that failed, an app misbehaving, or data you
        need to check.
      </p>

      <h2>A deploy failed</h2>
      <p>
        Open the deploy in <strong>Deployments</strong> and read its log. If the new version never
        became healthy, your old version keeps serving, and the log ends with the reason and the last
        30 lines your app printed. Skiff removes the failed container, so this log is the record of
        what happened.
      </p>
      <ul>
        <li>
          <strong>The app kept exiting:</strong> the output above it usually says why, such as a
          missing variable or a database it couldn&apos;t reach.
        </li>
        <li>
          <strong>It kept running but never answered:</strong> check that it listens on{" "}
          <code>0.0.0.0</code> and on the port in your settings, not <code>127.0.0.1</code>.
        </li>
      </ul>
      <Callout>
        Deploy logs can include whatever your app printed, and the{" "}
        <a href="/docs/github-actions">GitHub Action</a> repeats a failed deploy&apos;s log in your
        pipeline. On a public repository, anyone can read that, so don&apos;t print secrets at
        startup.
      </Callout>

      <h2>Read logs</h2>
      <p>
        Choose <strong>Logs</strong> on a project. Live output comes from every copy of the app and
        its workers, with each line labelled when there&apos;s more than one. History keeps the last
        7 days, and you can search it or show only errors. Each deploy also has a Logs link for the 30
        minutes after it went out.
      </p>
      <p>From your terminal, after <code>skiff login</code>:</p>
      <CodeBlock label="shell">{`skiff logs shop-api          # the last 100 lines
skiff logs shop-api -f       # keep streaming
skiff logs shop-api -n 500   # more history`}</CodeBlock>

      <h2>Open a shell</h2>
      <p>
        <strong>Console</strong> on a project opens a shell inside the running app. Workers have their
        own console button under <strong>Jobs &amp; workers</strong>. If your app has several copies, you get the first one.
        Images without a shell, such as distroless ones, can&apos;t open a console.
      </p>
      <p>
        Changes you make there are lost on the next deploy. Every team member can open a console, and
        each one is recorded under <strong>Activity</strong>.
      </p>

      <h2>Reach a database</h2>
      <p>
        <strong>Shell</strong> on a database opens <code>psql</code>, <code>redis-cli</code> or your
        engine&apos;s own client, already signed in, with nothing exposed to the internet. This is
        also recorded under Activity.
      </p>
      <p>
        To connect from a desktop client, turn on <strong>Public access</strong> and copy the public
        address. Your database then accepts connections from the whole internet, protected by its
        password, so turn it off when you finish. Doing either restarts the database briefly.
      </p>
    </article>
  );
}
