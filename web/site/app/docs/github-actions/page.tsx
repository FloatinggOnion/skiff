import type { Metadata } from "next";
import { Callout, Steps, Step } from "@/components/docs-prose";
import { CodeBlock } from "@/components/code-block";

export const metadata: Metadata = {
  title: "GitHub Actions",
  description:
    "Deploy to your Skiff server from a workflow, and test every pull request against its own preview environment.",
  alternates: { canonical: "/docs/github-actions" },
};

export default function GithubActions() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">Guides</p>
      <h1>GitHub Actions</h1>
      <p className="text-muted text-lg">
        Deploy from a workflow once your tests pass, or give each pull request its own environment to
        test against. The step waits for the deploy, and fails if the deploy fails.
      </p>

      <h2>Set it up</h2>
      <Steps>
        <Step n={1} title="Create an API token">
          In the dashboard, open <strong>Settings → Integrations</strong> and create one. It can reach
          everything in that team.
        </Step>
        <Step n={2} title="Add two repository secrets">
          In GitHub, add <code>SKIFF_SERVER</code> with your dashboard address and{" "}
          <code>SKIFF_TOKEN</code> with the token.
        </Step>
        <Step n={3} title="Turn off automatic deploys">
          So a push doesn&apos;t deploy before your tests finish, turn off automatic deploys in the
          project&apos;s settings.
        </Step>
      </Steps>

      <h2>Deploy after your tests pass</h2>
      <CodeBlock label=".github/workflows/deploy.yml">{`on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: npm test
      - uses: nuelScript/skiff@v0
        with:
          server: \${{ secrets.SKIFF_SERVER }}
          token: \${{ secrets.SKIFF_TOKEN }}
          app: shop-api`}</CodeBlock>
      <Callout>
        Skiff deploys the newest commit on the project&apos;s branch. If someone pushes while your tests
        run, that newer commit is what goes out, and the step adds a notice saying so.
      </Callout>

      <h2>Deploy a prebuilt image</h2>
      <p>
        For a project that <a href="/docs/images">runs a prebuilt image</a>, pass the tag you just pushed:
      </p>
      <CodeBlock label="step">{`- uses: nuelScript/skiff@v0
  with:
    server: \${{ secrets.SKIFF_SERVER }}
    token: \${{ secrets.SKIFF_TOKEN }}
    app: shop-api
    image: ghcr.io/you/shop-api:\${{ github.sha }}`}</CodeBlock>

      <h2>Test each pull request in a preview</h2>
      <p>
        Create a preview of the branch, run your tests against its address, then remove it. Each preview
        gets empty copies of its project&apos;s volumes and uses the project&apos;s environment variables,
        so <a href="/docs/previews">give previews their own database</a> to keep tests away from
        production.
      </p>
      <CodeBlock label=".github/workflows/preview.yml">{`on: pull_request

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - id: preview
        uses: nuelScript/skiff@v0
        with:
          server: \${{ secrets.SKIFF_SERVER }}
          token: \${{ secrets.SKIFF_TOKEN }}
          app: shop-api
          command: preview
      - run: npm run test:e2e
        env:
          BASE_URL: \${{ steps.preview.outputs.url }}
      - uses: nuelScript/skiff@v0
        if: always()
        with:
          server: \${{ secrets.SKIFF_SERVER }}
          token: \${{ secrets.SKIFF_TOKEN }}
          app: shop-api
          command: remove-preview`}</CodeBlock>

      <h2>Inputs</h2>
      <ul>
        <li>
          <code>server</code>, <code>token</code> and <code>app</code> are required.
        </li>
        <li>
          <code>command</code> is <code>deploy</code> (the default), <code>preview</code> or{" "}
          <code>remove-preview</code>.
        </li>
        <li>
          <code>image</code> is a new image for a project that runs one.
        </li>
        <li>
          <code>branch</code> is the preview&apos;s branch. It defaults to the branch the workflow runs on.
        </li>
        <li>
          <code>timeout</code> is how many seconds to wait. The default is 900.
        </li>
      </ul>

      <h2>Outputs</h2>
      <ul>
        <li>
          <code>url</code> is the app&apos;s public address.
        </li>
        <li>
          <code>app</code> is the name of what deployed, which for a preview is its own name.
        </li>
        <li>
          <code>commit</code> and <code>deploy-id</code> identify the deploy.
        </li>
      </ul>
      <p>
        Your server has to be reachable from GitHub&apos;s runners. Anything else can use the{" "}
        <a href="/docs/api">API</a> directly.
      </p>
    </article>
  );
}
