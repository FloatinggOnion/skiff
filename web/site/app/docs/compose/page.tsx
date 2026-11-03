import type { Metadata } from "next";
import { Callout, Steps, Step } from "@/components/docs-prose";
import { CodeBlock } from "@/components/code-block";

export const metadata: Metadata = {
  title: "Docker Compose",
  description:
    "Bring a whole backend onto Skiff from your existing Compose file. Services keep finding each other by the same names.",
  alternates: { canonical: "/docs/compose" },
};

export default function ComposeDocs() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">Guides</p>
      <h1>Docker Compose</h1>
      <p className="text-muted text-lg">
        Bring an API, a worker, a database and Redis onto Skiff from the Compose file you already
        have. Skiff turns each service into a project, worker or managed database, so your stack gets
        backups, rollbacks and the dashboard. Services keep finding each other by the same names, so{" "}
        <code>db:5432</code> in your config keeps working.
      </p>

      <h2>Check a file first</h2>
      <p>This shows what your file would become, and changes nothing:</p>
      <CodeBlock label="terminal">{`skiff compose plan
skiff compose plan -f deploy/compose.yml --name shop`}</CodeBlock>

      <h2>Create a stack</h2>
      <Steps>
        <Step n={1} title="Open Stacks in the dashboard">
          Choose <strong>New stack</strong>, then give a repository or paste your file. Services that
          build from source need a repository.
        </Step>
        <Step n={2} title="Check the file">
          Skiff lists what it will create, what will work differently and anything to fix first.
        </Step>
        <Step n={3} title="Fill in missing values">
          Values like <code>$&#123;DB_PASSWORD&#125;</code> come from what you enter here, then from a{" "}
          <code>.env</code> file beside the Compose file. Check again after filling them in.
        </Step>
        <Step n={4} title="Create it">
          Databases start first, then apps deploy in <code>depends_on</code> order.
        </Step>
      </Steps>

      <h2>Change a stack</h2>
      <p>
        For a stack from a GitHub repository, push your Compose file. Safe changes apply on their own,
        and only the services that changed are redeployed. A push that would delete data, or that has a
        problem, waits for you: the stack shows why, you get an alert, and new code still deploys with
        the current settings. Review those with <strong>Update</strong>. For a pasted file, or another
        Git host, use <strong>Update</strong> for every change. Values you entered earlier are remembered,
        encrypted, so it only asks for new ones.
      </p>
      <ul>
        <li>
          <strong>Deleting data needs your OK.</strong> Removing a database, a volume or an app with a
          volume is only applied once you confirm it.
        </li>
        <li>
          <strong>Database images and credentials can&apos;t be changed this way.</strong> A new password
          or major version doesn&apos;t apply to existing data, so Skiff refuses the update and explains
          why. To change one, back up and restore into a new database service.
        </li>
        <li>
          <strong>Your file wins.</strong> Settings or environment variables you changed in the dashboard
          are replaced if they differ from the file, and the update lists them first.
        </li>
      </ul>

      <h2>What each service becomes</h2>
      <ul>
        <li>
          <strong>Stock Postgres, MySQL, MongoDB and Redis images</strong> become managed databases
          with daily backups, using your file&apos;s image, username, password and database name. They
          stay private. Other images, like <code>pgvector/pgvector</code>, run as ordinary private
          apps, and their data is covered by volume backups.
        </li>
        <li>
          <strong>Services that publish a port</strong> become public apps with HTTPS. Skiff serves
          one port per app, so only the first is used.
        </li>
        <li>
          <strong>Services with no published port</strong> stay private, and only the stack can reach
          them.
        </li>
        <li>
          <strong>A service with the same build, environment and volumes as an app</strong>, but its
          own command and no port, becomes that app&apos;s worker.
        </li>
      </ul>

      <h2>What&apos;s different from Compose</h2>
      <ul>
        <li>
          <strong>Folders on your machine</strong>, like <code>./nginx.conf:/etc/nginx.conf</code>,
          can&apos;t be used because they won&apos;t exist on the server. Build the file into the image or
          use a named volume.
        </li>
        <li>
          <strong>Volumes</strong> can only be shared between an app and its workers.
        </li>
        <li>
          <strong>Health checks</strong> are used when they make an HTTP request. Otherwise a private
          service counts as ready once its port opens.
        </li>
        <li>
          <strong>Not supported:</strong> <code>privileged</code>, <code>network_mode</code>,{" "}
          <code>devices</code>, <code>extends</code>, <code>secrets</code> and <code>configs</code>.
          Keys like <code>restart</code> and <code>networks</code> are ignored.
        </li>
      </ul>

      <Callout>
        Previews aren&apos;t available for a stack&apos;s services, and they can&apos;t reach your team&apos;s
        other databases and buckets yet. Pushes only reach Skiff with GitHub connected, and an access token
        is only used when you create or update the stack, so connect GitHub for private repositories.
      </Callout>
    </article>
  );
}
