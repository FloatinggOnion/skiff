import type { Metadata } from "next";
import { Callout, Steps, Step } from "@/components/docs-prose";
import { CodeBlock } from "@/components/code-block";

export const metadata: Metadata = {
  title: "Self-hosting",
  description: "Stand up the Skiff dashboard and edge router on your own server with one command.",
  alternates: { canonical: "/docs/self-hosting" },
};

export default function SelfHosting() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand-2/80 font-mono text-[11px] tracking-[0.3em] uppercase">
        Getting started
      </p>
      <h1>Self-hosting</h1>
      <p className="text-muted text-lg">
        One command turns a fresh Linux server into your own deploy platform — the web dashboard, an
        edge router with automatic HTTPS, and everything needed to push apps to it.
      </p>

      <h2>What you&apos;ll need</h2>
      <ul>
        <li>
          A fresh <strong>Ubuntu or Debian</strong> server with a public IP and root SSH access (a
          $5/mo VPS is plenty to start).
        </li>
        <li>
          A <strong>domain</strong> you can add DNS records to. Apps are served at{" "}
          <code>&lt;app&gt;.yourdomain.com</code> and the dashboard at <code>dash.yourdomain.com</code>.
        </li>
      </ul>

      <h2>Install</h2>
      <p>
        SSH into your server and run this as root — replace <code>example.com</code> with your domain:
      </p>
      <CodeBlock label="on your server">
        curl -fsSL https://useskiff.xyz/install | sh -s -- --domain example.com
      </CodeBlock>
      <p>The installer:</p>
      <ul>
        <li>installs Docker (if it isn&apos;t already),</li>
        <li>
          downloads the latest <code>skiff</code> binary,
        </li>
        <li>
          sets up systemd services for the <strong>edge router</strong> (binds :80 / :443 with
          Let&apos;s Encrypt) and the <strong>control panel</strong> (zero-downtime, blue-green),
        </li>
        <li>generates a one-time setup key and prints your dashboard URL.</li>
      </ul>
      <Callout>
        It finishes by printing your DNS records, dashboard URL, and setup key. Keep that key — you
        need it for the first login.
      </Callout>

      <h2>Point your DNS</h2>
      <p>
        Add these records at your DNS provider, pointing at your server&apos;s IP (the installer
        prints it). The wildcard covers every app and the dashboard in one record:
      </p>
      <CodeBlock label="dns">{`A    *.example.com    <your-server-ip>
A    example.com      <your-server-ip>    (optional — apex / marketing site)`}</CodeBlock>
      <p>
        Certificates are issued automatically on the first HTTPS request, so there&apos;s nothing to
        configure — just wait for DNS to propagate (usually a few minutes).
      </p>

      <h2>First login</h2>
      <Steps>
        <Step n={1} title="Open your dashboard">
          Visit <code>https://dash.example.com</code> once DNS resolves. The first visit asks you to
          create the owner account.
        </Step>
        <Step n={2} title="Enter the setup key">
          Paste the setup key the installer printed, then set your email and password. That&apos;s the
          owner account — teammates join later by invite.
        </Step>
        <Step n={3} title="Deploy your first app">
          Click <strong>Deploy</strong>, paste a Git repository URL (or connect GitHub), pick a name
          and port, and Skiff builds and runs it at <code>&lt;name&gt;.example.com</code> with its own
          certificate.
        </Step>
      </Steps>

      <h2>Connect GitHub (optional)</h2>
      <p>
        For push-to-deploy — a <code>git push</code> rebuilds and ships automatically, and branches
        can spin up preview environments — install the Skiff GitHub App from{" "}
        <strong>Settings → GitHub</strong> in the dashboard and pick which repos it can access.
        Without it, you can still deploy any public repo (or a private one with a token) by pasting
        its URL.
      </p>

      <h2>Updating</h2>
      <p>
        Re-run the installer any time to update to the latest release. Your data, apps, and setup key
        are preserved — it swaps the binary and restarts the services:
      </p>
      <CodeBlock label="on your server">
        curl -fsSL https://useskiff.xyz/install | sh -s -- --domain example.com
      </CodeBlock>

      <h2>Troubleshooting</h2>
      <p>Check the services and their logs over SSH:</p>
      <CodeBlock label="on your server">{`systemctl status skiff-router skiff-panel@7070
journalctl -u skiff-router -n 50 --no-pager
journalctl -u skiff-panel@7070 -n 50 --no-pager`}</CodeBlock>
      <ul>
        <li>
          <strong>Dashboard won&apos;t load / no certificate:</strong> confirm{" "}
          <code>dash.example.com</code> resolves to your server&apos;s IP. Certs issue on the first
          request once DNS is correct.
        </li>
        <li>
          <strong>An app 404s:</strong> check it&apos;s running under <strong>Server</strong> in the
          dashboard, and that <code>&lt;app&gt;.example.com</code> is covered by your wildcard record.
        </li>
        <li>
          <strong>Ports 80/443 in use:</strong> make sure nothing else (nginx, Apache, Caddy) is
          bound to them — the Skiff router needs both.
        </li>
        <li>
          <strong>Locked out by two-factor:</strong> if you&apos;ve lost both your authenticator and
          your recovery codes, turn two-factor off for your account from the server, then sign in
          with your password and set it up again:
          <CodeBlock label="on your server">skiff server reset-2fa you@example.com</CodeBlock>
        </li>
      </ul>
    </article>
  );
}
