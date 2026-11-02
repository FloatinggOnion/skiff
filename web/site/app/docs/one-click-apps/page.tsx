import type { Metadata } from "next";
import Link from "next/link";
import { Callout, Steps, Step } from "@/components/docs-prose";

export const metadata: Metadata = {
  title: "One-click apps",
  description:
    "Install Uptime Kuma, n8n, Ghost, Umami, Vaultwarden or Gitea on your own server, with storage, secrets and any database set up for you.",
  alternates: { canonical: "/docs/one-click-apps" },
};

const APPS = [
  ["Uptime Kuma", "Status pages and uptime monitoring", "256 MB", "Create the admin account"],
  ["n8n", "Workflow automation", "512 MB", "Create the owner account"],
  ["Ghost", "Blog and newsletters, with MySQL", "1 GB", "Open /ghost and create the owner account"],
  ["Umami", "Website analytics, with Postgres", "512 MB", "Sign in as admin / umami and change the password"],
  ["Vaultwarden", "Password manager for the Bitwarden apps", "128 MB", "Create your account, then turn off sign-ups"],
  ["Gitea", "Git hosting with issues and pull requests", "256 MB", "Finish the install screen and create the admin"],
];

export default function OneClickApps() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">Guides</p>
      <h1>One-click apps</h1>
      <p className="text-muted text-lg">
        Install a ready-made app on your server. Skiff picks a tested version and sets up its
        storage, its secrets and any database it needs.
      </p>

      <h2>Install one</h2>
      <Steps>
        <Step n={1} title="Choose New project → One-click apps">
          Pick an app and give it a name. The name becomes its address, like{" "}
          <code>blog.yourdomain.com</code>.
        </Step>
        <Step n={2} title="Install, and wait for its first start">
          The first install downloads the image, so it can take a few minutes. The log shows how
          far along it is.
        </Step>
        <Step n={3} title="Do its first-run step straight away">
          The project page shows what to do until you mark it done.
        </Step>
      </Steps>
      <Callout>
        Don&apos;t skip the first-run step. These apps are on the public internet from the moment
        they start, and on most of them the first visitor becomes the admin. Umami starts with a
        well-known password, and Vaultwarden lets anyone sign up until you turn that off.
      </Callout>

      <h2>What&apos;s available</h2>
      <div className="not-prose border-line overflow-x-auto rounded-xl border">
        <table className="w-full min-w-[640px] text-left text-sm">
          <thead className="text-subtle">
            <tr className="border-line border-b">
              <th className="px-4 py-2.5 font-medium">App</th>
              <th className="px-4 py-2.5 font-medium">What it is</th>
              <th className="px-4 py-2.5 font-medium">Needs about</th>
              <th className="px-4 py-2.5 font-medium">First-run step</th>
            </tr>
          </thead>
          <tbody className="text-muted">
            {APPS.map(([name, what, mem, step]) => (
              <tr key={name} className="border-line border-b last:border-0">
                <td className="text-fg px-4 py-2.5 font-medium">{name}</td>
                <td className="px-4 py-2.5">{what}</td>
                <td className="px-4 py-2.5 whitespace-nowrap">{mem}</td>
                <td className="px-4 py-2.5">{step}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <h2>After installing</h2>
      <ul>
        <li>
          <strong>It&apos;s a normal project.</strong> Logs, metrics, domains, limits and rollback
          all work, and its settings are yours to change.
        </li>
        <li>
          <strong>Files and databases stay across deploys.</strong> App data sits on a{" "}
          <Link href="/docs/volumes">persistent volume</Link> or in a managed database. Back up
          anything you can&apos;t lose: managed databases get daily backups, and volumes don&apos;t.
        </li>
        <li>
          <strong>Upgrades are up to you.</strong> The version is pinned, so an app only changes
          when you change its image under <strong>Settings</strong> and redeploy. Check the
          app&apos;s release notes first.
        </li>
        <li>
          <strong>Adding a custom domain?</strong> Ghost, n8n, Vaultwarden and Gitea store their
          address in an environment variable, such as <code>url</code>, <code>WEBHOOK_URL</code>,{" "}
          <code>DOMAIN</code> or <code>GITEA__server__ROOT_URL</code>. Update it under{" "}
          <strong>Environment</strong> and redeploy.
        </li>
        <li>
          <strong>Deleting the project keeps its database.</strong> Delete that separately under{" "}
          <strong>Databases</strong> when you&apos;re sure you don&apos;t need the data.
        </li>
      </ul>
    </article>
  );
}
