import type { Metadata } from "next";
import Link from "next/link";
import { Callout, Steps, Step } from "@/components/docs-prose";

export const metadata: Metadata = {
  title: "Preview environments",
  description:
    "Give every branch its own live URL, posted on its pull request, kept away from production data and optionally behind a password.",
  alternates: { canonical: "/docs/previews" },
};

export default function Previews() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">
        Guides
      </p>
      <h1>Preview environments</h1>
      <p className="text-muted text-lg">
        Every branch can get its own copy of a project at its own URL, with its own certificate, so
        you can click through a change before it reaches production.
      </p>

      <h2>Turn them on</h2>
      <Steps>
        <Step n={1} title="Connect GitHub">
          Install the Skiff GitHub App from <strong>Settings → Integrations</strong> and give it the
          repositories you deploy from.
        </Step>
        <Step n={2} title="Enable previews on the project">
          In the project&apos;s <strong>Settings</strong>, switch on{" "}
          <strong>Preview deployments</strong>.
        </Step>
        <Step n={3} title="Push a branch">
          A push to any branch other than the production one builds a preview at{" "}
          <code>&lt;project&gt;-&lt;branch&gt;.&lt;your domain&gt;</code>. Later pushes to that
          branch redeploy it.
        </Step>
      </Steps>
      <p>
        You can also create one by hand from the project&apos;s <strong>Previews</strong> tab for
        any branch.
      </p>

      <h2>On your pull requests</h2>
      <ul>
        <li>
          <strong>A comment with the link.</strong> Skiff posts the preview URL on the branch&apos;s
          pull request, whether the pull request is opened before or after the preview is built,
          and keeps that one comment up to date on every push.
        </li>
        <li>
          <strong>A status next to your checks.</strong> Every deploy reports on its commit as{" "}
          <code>Skiff / &lt;project&gt;</code> (or <code>… preview</code>): building, then live
          with a link to the app, or failed with a link to the build log. It works for production
          deploys too.
        </li>
      </ul>
      <Callout>
        Both need the GitHub App to have <strong>Pull requests</strong> and{" "}
        <strong>Commit statuses</strong> set to read and write. New installs ask for them; an older
        install shows a link under <strong>Settings → Integrations</strong> to grant them.
      </Callout>

      <h2>Only what the branch changed</h2>
      <p>
        If a repository holds several projects, each in its own root directory, a branch only gets
        previews for the projects whose directory it touched. A branch that only changes the API
        doesn&apos;t build a preview of the website.
      </p>

      <h2>Keep previews away from production data</h2>
      <p>
        Previews use the project&apos;s environment variables, read fresh on every deploy. Anything
        under <strong>Environment → Preview overrides</strong> replaces a variable of the same name
        for previews only. Point <code>DATABASE_URL</code> at a staging database there, or switch an
        API key to test mode, and a preview never touches what production uses.
      </p>
      <p>
        If the project has <Link href="/docs/volumes">volumes</Link>, a preview mounts them at the same
        paths, on empty volumes of its own.
      </p>

      <h2>Password protection</h2>
      <p>
        On the <strong>Previews</strong> tab, set a password and every preview of the project asks
        for it before showing anything. Your production URL stays public. A visitor stays signed in
        to a preview for a week; changing the password signs everyone out. Repeated wrong guesses
        from one address are blocked for a few minutes.
      </p>

      <h2>Cleaning up</h2>
      <p>
        Deleting a branch removes its previews: containers, images and routes. If your repository
        deletes branches when a pull request merges, previews take care of themselves. You can also
        remove one from the <strong>Previews</strong> tab at any time.
      </p>
    </article>
  );
}
