import type { Metadata } from "next";
import { Callout, Steps, Step } from "@/components/docs-prose";
import { CodeBlock } from "@/components/code-block";

export const metadata: Metadata = {
  title: "AI agents (MCP)",
  description:
    "Let an AI assistant deploy, read logs and traffic, change env vars and roll back your Skiff projects over the Model Context Protocol.",
  alternates: { canonical: "/docs/agents" },
};

export default function Agents() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">
        Guides
      </p>
      <h1>AI agents (MCP)</h1>
      <p className="text-muted text-lg">
        <code>skiff mcp</code> serves your projects over the Model Context Protocol, so an
        assistant in your editor or terminal can ship and debug them with you.
      </p>

      <h2>Connect an assistant</h2>
      <Steps>
        <Step n={1} title="Create an API token">
          In the dashboard, open <strong>Settings → Integrations</strong> and create one. It&apos;s
          shown once and acts for that team only.
        </Step>
        <Step n={2} title="Add Skiff as an MCP server">
          Most clients take a block like this:
          <CodeBlock label="mcp config">{`{
  "mcpServers": {
    "skiff": {
      "command": "skiff",
      "args": ["mcp"],
      "env": {
        "SKIFF_URL": "https://dash.example.com",
        "SKIFF_TOKEN": "skiff_..."
      }
    }
  }
}`}</CodeBlock>
          From a terminal client, the same thing is usually one command:
          <CodeBlock label="shell">{`claude mcp add skiff \\
  --env SKIFF_URL=https://dash.example.com \\
  --env SKIFF_TOKEN=skiff_... \\
  -- skiff mcp`}</CodeBlock>
        </Step>
        <Step n={3} title="Ask">
          <em>“Why is checkout throwing 500s since this morning?”</em> —{" "}
          <em>“Deploy the API and tell me if it comes up.”</em> —{" "}
          <em>“Roll the site back to yesterday&apos;s build.”</em>
        </Step>
      </Steps>

      <h2>What it can do</h2>
      <ul>
        <li>
          <strong>list_projects</strong>, <strong>get_project</strong> — state, URL, replicas and the
          last deploy.
        </li>
        <li>
          <strong>deploy</strong> — build the latest commit and wait for it, returning the end of the
          build log.
        </li>
        <li>
          <strong>list_deploys</strong>, <strong>deploy_log</strong>, <strong>rollback</strong> —
          history, build output, and putting an earlier build back without rebuilding.
        </li>
        <li>
          <strong>app_logs</strong> — the running app&apos;s recent output.
        </li>
        <li>
          <strong>app_traffic</strong> — requests, error rate, latency, top paths, error paths, the
          deploys in that window, and uptime incidents.
        </li>
        <li>
          <strong>get_env</strong>, <strong>set_env</strong> — read and change environment
          variables. Values stay hidden unless the assistant asks for one variable by name. Changes
          merge with what&apos;s there and apply on the next deploy.
        </li>
        <li>
          <strong>list_workers</strong>, <strong>set_worker</strong>, <strong>remove_worker</strong>:
          background workers and how many copies run.
        </li>
        <li>
          <strong>list_jobs</strong>, <strong>add_job</strong>, <strong>run_job</strong>,{" "}
          <strong>remove_job</strong>: scheduled jobs, and running one now to see its output.
        </li>
        <li>
          <strong>list_databases</strong>, <strong>create_database</strong>,{" "}
          <strong>attach_database</strong>, <strong>detach_database</strong>, and the same four for
          buckets: create them and give a project their address and keys.
        </li>
        <li>
          <strong>list_volumes</strong>, <strong>add_volume</strong>: storage that keeps its data
          across deploys.
        </li>
      </ul>
      <p>
        No tool deletes a database, bucket or volume, so an assistant can&apos;t lose your data. Do
        that yourself in the dashboard or with the CLI. Attaching and detaching need the sign-in from{" "}
        <code>skiff login</code>; a token made in the dashboard is refused.
      </p>
      <p>
        Logs, build output and traffic include text written by visitors and repo authors. Skiff
        fences it and tells the assistant to treat it as data, but a read-only connection is the
        safer default for an assistant that reads logs.
      </p>

      <h2>Read-only mode</h2>
      <p>
        Start the server with <code>--read-only</code>, or set <code>SKIFF_MCP_READONLY=1</code>, and
        every tool that changes something is left out: deploys, rollbacks, variables, workers, jobs,
        databases, buckets and volumes. The assistant can still look at projects, logs and traffic.
      </p>
      <CodeBlock label="shell">{`claude mcp add skiff \
  --env SKIFF_URL=https://dash.example.com \
  --env SKIFF_TOKEN=skiff_... \
  --env SKIFF_MCP_READONLY=1 \
  -- skiff mcp`}</CodeBlock>
      <Callout>
        The token can deploy, roll back and read secrets for its whole team. Give each
        assistant its own token so you can see its actions in <strong>Activity</strong> and revoke
        it on its own.
      </Callout>
    </article>
  );
}
