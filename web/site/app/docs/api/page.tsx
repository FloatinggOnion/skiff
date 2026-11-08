import type { Metadata } from "next";
import { CodeBlock } from "@/components/code-block";

export const metadata: Metadata = {
  title: "API reference",
  description: "Deploy, roll back, manage previews, workers, scheduled jobs, databases, buckets, volumes and environment variables, and read logs over Skiff's HTTP API.",
  alternates: { canonical: "/docs/api" },
};

const groups: { title: string; rows: [string, string, string][] }[] = [
  {
    title: "Apps",
    rows: [
      ["GET", "/apps", "List the team's apps."],
      ["GET", "/apps/{name}", "One app: state, address, replicas and latest commit."],
      ["GET", "/apps/{name}/logs", "Recent output from the app and its workers. Add ?lines= up to 2000, or ?follow=1 to stream it as plain text."],
      ["GET", "/apps/{name}/insights", "Traffic and uptime. Add ?range= with 1h, 24h or 7d. The default is 24h."],
    ],
  },
  {
    title: "Deploys",
    rows: [
      ["POST", "/apps/{name}/deploy", 'Deploy the newest commit. For an image project, send {"image": "..."} to change the image.'],
      ["GET", "/apps/{name}/deploys", "The app's history, marking which can be rolled back to."],
      ["POST", "/apps/{name}/rollback", 'Go back to an earlier deploy. Send {"deploy": "<id>"}.'],
      ["GET", "/deploys/{id}", "A deploy's status: building, live, failed or canceled."],
      ["GET", "/deploys/{id}/log", "A deploy's status and the last 200 lines of its build log."],
    ],
  },
  {
    title: "Previews",
    rows: [
      ["POST", "/apps/{name}/previews", 'Create or redeploy a branch\'s preview. Send {"branch": "feat/login"}.'],
      ["DELETE", "/apps/{name}/previews?branch=", "Remove a branch's preview."],
    ],
  },
  {
    title: "Workers",
    rows: [
      ["GET", "/apps/{name}/workers", "List the app's workers, with how many are running."],
      ["PUT", "/apps/{name}/workers/{worker}", 'Create the worker or change it. Send {"command": "node queue.js", "replicas": 2}. Replicas go from 1 to 10.'],
      ["DELETE", "/apps/{name}/workers/{worker}", "Stop and remove the worker."],
    ],
  },
  {
    title: "Scheduled jobs",
    rows: [
      ["GET", "/apps/{name}/jobs", "List the app's jobs, with when each last ran, whether it worked and when it runs next."],
      ["POST", "/apps/{name}/jobs", 'Add a job. Send {"name": "report", "schedule": "0 3 * * *", "command": "node report.js"}. The schedule uses cron syntax, in the server\'s time zone.'],
      ["POST", "/apps/{name}/jobs/{id}/run", "Run the job now and wait for it. Answers with ok and its output."],
      ["DELETE", "/apps/{name}/jobs/{id}", "Remove the job."],
    ],
  },
  {
    title: "Databases",
    rows: [
      ["GET", "/databases", "List the team's databases: name, engine, state, port and the apps they're attached to."],
      ["POST", "/databases", 'Create one. Send {"engine": "postgres", "name": "shop-db"}. The engine is postgres, mysql, mongodb or redis.'],
      ["DELETE", "/databases/{name}", "Delete the database, its data and its backups. Needs a token from skiff login."],
      ["PUT", "/databases/{name}/apps/{app}", "Attach it to an app, which gets its address in a variable like DATABASE_URL on the next deploy. Needs a token from skiff login."],
      ["DELETE", "/databases/{name}/apps/{app}", "Detach it and remove the variable. Needs a token from skiff login."],
      ["GET", "/databases/{name}/connection?port=", "The connection address, including its password, for a tunnel on that local port. Needs a token from skiff login."],
      ["GET", "/databases/{name}/tunnel", "A WebSocket carrying one client connection to the database. Needs a token from skiff login."],
    ],
  },
  {
    title: "Buckets",
    rows: [
      ["GET", "/buckets", "List the team's buckets, with the apps they're attached to."],
      ["POST", "/buckets", 'Create one. Send {"name": "media"}, at least 3 lowercase letters, numbers or hyphens.'],
      ["DELETE", "/buckets/{name}", "Delete the bucket and every file in it. Needs a token from skiff login."],
      ["PUT", "/buckets/{name}/apps/{app}", "Attach it to an app, which gets S3_ENDPOINT, S3_BUCKET and its keys on the next deploy. Needs a token from skiff login."],
      ["DELETE", "/buckets/{name}/apps/{app}", "Detach it and remove those variables. Needs a token from skiff login."],
    ],
  },
  {
    title: "Volumes",
    rows: [
      ["GET", "/apps/{name}/volumes", "List the app's volumes, with their size."],
      ["POST", "/apps/{name}/volumes", 'Add one. Send {"name": "data", "path": "/data"}. It\'s mounted on the next deploy.'],
      ["DELETE", "/apps/{name}/volumes/{volume}", "Stop mounting it. Its data is deleted after the next deploy."],
    ],
  },
  {
    title: "Environment variables",
    rows: [
      ["GET", "/apps/{name}/env", "Read the app's variables, including their values."],
      ["PUT", "/apps/{name}/env", 'Replace all of them. Send {"vars": [{"key": "A", "value": "1", "build": false}]}. They apply on the next deploy.'],
    ],
  },
];

export default function ApiReference() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">Reference</p>
      <h1>API reference</h1>
      <p className="text-muted text-lg">
        Everything lives under <code>/api/v1</code> on your dashboard&apos;s address and answers in JSON.
      </p>

      <h2>Authentication</h2>
      <p>
        Create an API token in the dashboard under <strong>Settings → Integrations</strong>, and send it as a bearer
        token. It can reach everything in the team that created it, including environment variables,
        so treat it like a password. Connecting to a database, and deleting or attaching databases
        and buckets, needs a token from <code>skiff login</code> instead, so a token kept in CI
        can&apos;t reach your data.
      </p>
      <CodeBlock label="shell">{`curl https://dash.example.com/api/v1/apps \\
  -H "Authorization: Bearer $SKIFF_TOKEN"`}</CodeBlock>

      {groups.map((g) => (
        <section key={g.title}>
          <h2>{g.title}</h2>
          <div className="not-prose overflow-x-auto">
            <table className="w-full min-w-[560px] border-collapse text-left text-sm">
              <tbody>
                {g.rows.map(([method, path, about]) => (
                  <tr key={method + path} className="border-line border-b align-top">
                    <td className="text-muted py-2.5 pr-3 font-mono text-xs">{method}</td>
                    <td className="py-2.5 pr-4 font-mono text-xs whitespace-nowrap">{path}</td>
                    <td className="text-muted py-2.5">{about}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      ))}

      <h2>Deploying from a script</h2>
      <p>
        A deploy starts in the background. Starting one answers <code>202</code> with its ID, and you
        check it until it&apos;s no longer <code>building</code>.
      </p>
      <CodeBlock label="shell">{`curl -X POST https://dash.example.com/api/v1/apps/shop-api/deploy \\
  -H "Authorization: Bearer $SKIFF_TOKEN"
# {"app":"shop-api","id":"1793677298-2188f1","status":"building","url":"https://shop-api.example.com"}

curl https://dash.example.com/api/v1/deploys/1793677298-2188f1 \\
  -H "Authorization: Bearer $SKIFF_TOKEN"`}</CodeBlock>

      <h2>Errors</h2>
      <p>
        Errors answer with a status code and a message, like <code>{`{"error": "app not found"}`}</code>.
        A bad or missing token answers <code>401</code>. Another team&apos;s app answers <code>404</code>,
        the same as one that doesn&apos;t exist.
      </p>
      <p>
        On GitHub, the <a href="/docs/github-actions">Action</a> does this for you. To let an AI agent use
        Skiff, see <a href="/docs/agents">AI agents</a>.
      </p>
    </article>
  );
}
