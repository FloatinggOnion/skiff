import type { Metadata } from "next";
import Link from "next/link";
import { Callout, Steps, Step } from "@/components/docs-prose";
import { CodeBlock } from "@/components/code-block";

export const metadata: Metadata = {
  title: "Prebuilt images",
  description:
    "Run an image that's already built, from Docker Hub, GitHub Container Registry or any private registry, with the same zero-downtime deploys and rollback.",
  alternates: { canonical: "/docs/images" },
};

export default function Images() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">
        Guides
      </p>
      <h1>Prebuilt images</h1>
      <p className="text-muted text-lg">
        Skiff can run an image that&apos;s already built instead of building from source: one your
        CI pushes to a registry, or an off-the-shelf app like Uptime Kuma or n8n. Health checks,
        volumes, replicas, domains and rollback work the same as for an app Skiff builds.
      </p>

      <h2>From the dashboard</h2>
      <Steps>
        <Step n={1} title="Choose New project → Deploy a Docker image">
          Enter the image, like <code>louislam/uptime-kuma:1</code> or{" "}
          <code>ghcr.io/you/api:latest</code>, and a name for the app.
        </Step>
        <Step n={2} title="Leave the port empty, or set it">
          With no port, Skiff uses the one the image exposes, and falls back to 8080 if it
          doesn&apos;t expose any. Set it if your app listens somewhere else.
        </Step>
        <Step n={3} title="Add credentials if the image is private">
          A username and a password or access token for the registry. For GitHub Container
          Registry, that&apos;s your GitHub username and a token with <code>read:packages</code>.
          You can change them later under the project&apos;s <strong>Settings</strong>.
        </Step>
      </Steps>
      <p>
        <strong>Redeploy</strong> pulls the image again, so a tag that moves, like{" "}
        <code>latest</code>, picks up its new version. To switch to another tag, change the image
        in <strong>Settings</strong> and redeploy.
      </p>

      <h2>Or in skiff.toml</h2>
      <p>
        Set <code>image</code> under <code>[build]</code> and <code>skiff deploy</code> pulls it
        instead of building:
      </p>
      <CodeBlock label="skiff.toml">{`name = "kuma"

[build]
image = "louislam/uptime-kuma:1"
# port = 3001   # optional: defaults to the port the image exposes

[[volumes]]
name = "data"
path = "/app/data"`}</CodeBlock>
      <p>
        For a private image, sign in with <code>docker login</code> on the machine that deploys, or
        set <code>SKIFF_REGISTRY_USER</code> and <code>SKIFF_REGISTRY_PASSWORD</code>, which are
        used for that one pull and never saved.
      </p>

      <h2>Deploy a new tag from CI</h2>
      <p>
        After your pipeline pushes an image, point the project at it and deploy in one call. Create
        a token under <strong>Settings → Integrations</strong>, then:
      </p>
      <CodeBlock label="shell">{`curl -X POST https://dash.example.com/api/v1/apps/api/deploy \\
  -H "Authorization: Bearer $SKIFF_TOKEN" \\
  -H "Content-Type: application/json" \\
  -d '{"image": "ghcr.io/you/api:'"$GITHUB_SHA"'"}'`}</CodeBlock>
      <p>
        The response has the deploy&apos;s <code>id</code>; poll{" "}
        <code>/api/v1/deploys/&lt;id&gt;</code> for its status. Leave out the body to redeploy the
        image the project already runs.
      </p>

      <h2>What&apos;s different</h2>
      <ul>
        <li>
          <strong>No previews.</strong> Previews deploy a branch of a repository, so an image project
          doesn&apos;t have them. The{" "}
          <Link href="/docs/previews">preview environments</Link> guide covers projects that build
          from Git.
        </li>
        <li>
          <strong>Environment variables</strong> are all set when the container starts. There&apos;s
          no build step to pass them to.
        </li>
        <li>
          <strong>Rollback</strong> keeps the last five images you deployed, so it works even after
          a tag has moved on in the registry.
        </li>
      </ul>
      <Callout>
        The release command, jobs and workers run with <code>sh -c</code> inside the image. Images
        without a shell, such as distroless ones, can serve traffic but can&apos;t run those.
      </Callout>
    </article>
  );
}
