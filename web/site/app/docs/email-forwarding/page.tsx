import type { Metadata } from "next";
import { Callout, Steps, Step } from "@/components/docs-prose";
import { CodeBlock } from "@/components/code-block";

export const metadata: Metadata = {
  title: "Email forwarding",
  description:
    "Forward mail sent to your own domain — hello@yourdomain.com — straight to any inbox, with no mailbox to host.",
  alternates: { canonical: "/docs/email-forwarding" },
};

export default function EmailForwarding() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">
        Guides
      </p>
      <h1>Email forwarding</h1>
      <p className="text-muted text-lg">
        Point your domain&apos;s mail at your server and Skiff hands every message straight on to the
        inbox you already use. No mailboxes to host, no IMAP, nothing to back up.
      </p>

      <h2>Set up a forward</h2>
      <Steps>
        <Step n={1} title="Add the address">
          In the dashboard, open <strong>Email</strong> and fill in the address you want
          (<code>hello</code>), your domain, and the inbox it should land in. Several inboxes work
          too — separate them with commas.
        </Step>
        <Step n={2} title="Point MX at your server">
          Add the MX record the page shows you, at your DNS provider:
          <CodeBlock label="dns">{`MX    @    mx.example.com    (priority 10)`}</CodeBlock>
          Mail follows MX records, so remove any others on that domain — otherwise messages keep
          going wherever those point.
        </Step>
        <Step n={3} title="Add the SPF record">
          The <code>TXT</code> record on the same page tells the receiving side that your server is
          allowed to pass mail along. Without it, forwarded messages are more likely to be treated as
          spam. If you already have an SPF record, add the <code>ip4:</code> term to that one rather
          than creating a second.
        </Step>
      </Steps>
      <p>
        The page turns green once it sees the records. DNS usually propagates in a few minutes — it
        rechecks on its own.
      </p>

      <h2>Addresses you can forward</h2>
      <ul>
        <li>
          <strong>A single address</strong> — <code>hello@example.com</code> to your inbox.
        </li>
        <li>
          <strong>A catch-all</strong> — use <code>*</code> as the address to take everything sent to
          the domain, whatever the recipient.
        </li>
        <li>
          <strong>Tagged addresses</strong> — <code>hello+invoices@example.com</code> follows the{" "}
          <code>hello</code> forward, so you can filter on the tag in your inbox.
        </li>
      </ul>
      <Callout>
        Forwarding is one-way: it delivers mail <em>to</em> your inbox, it doesn&apos;t let you send{" "}
        <em>as</em> the address. Replies go out from whatever inbox you read them in.
      </Callout>

      <h2>How delivery works</h2>
      <p>
        Your server accepts mail on port 25 (over TLS when the sender offers it), for your forwarded
        addresses only — it will never relay for anyone else. Each message is passed on while the
        sender is still connected, so nothing queues up on your server: if the destination is
        temporarily unavailable, the sending server is asked to retry and does the waiting for you.
      </p>
      <p>
        The message itself is handed over untouched, so it arrives from the original sender. Only the
        envelope is rewritten, which is what keeps SPF checks passing at the far end.
      </p>

      <h2>If your provider blocks port 25</h2>
      <p>
        Many hosts block outbound port 25 on new accounts, which stops forwarded mail from leaving
        the server. You can either ask your provider to unblock it, or send through an SMTP relay —
        the same settings Skiff uses for alert emails:
      </p>
      <CodeBlock label="/etc/skiff/skiff.env">{`SKIFF_SMTP_HOST=smtp.provider.com
SKIFF_SMTP_PORT=587
SKIFF_SMTP_USER=...
SKIFF_SMTP_PASS=...
SKIFF_SMTP_FROM=forward@example.com`}</CodeBlock>
      <p>
        Then restart the router with <code>systemctl restart skiff-router</code>. Relays refuse to
        send as domains they don&apos;t own, so messages arrive from your relay address as{" "}
        <em>“Sender via hello@example.com”</em>, with <code>Reply-To</code> set to the original
        sender — replying still reaches them.
      </p>

      <h2>Troubleshooting</h2>
      <ul>
        <li>
          <strong>Nothing arrives:</strong> check port 25 is reachable from outside, and watch the
          router log while you send a test —{" "}
          <code>journalctl -u skiff-router -f | grep mail</code>.
        </li>
        <li>
          <strong>Mail lands in spam:</strong> add the SPF record, and set reverse DNS (PTR) for your
          server&apos;s IP to <code>mx.example.com</code> in your provider&apos;s control panel.
        </li>
        <li>
          <strong>The sender gets a bounce:</strong> a “relaying denied” or “no such user” reply means
          the address isn&apos;t forwarded — check for a typo, or add a catch-all.
        </li>
      </ul>
    </article>
  );
}
