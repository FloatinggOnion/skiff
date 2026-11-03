import type { Metadata } from "next";
import { Callout } from "@/components/docs-prose";
import { CodeBlock } from "@/components/code-block";

export const metadata: Metadata = {
  title: "APIs and backends",
  description:
    "Run APIs and internal services on Skiff: private services, internal addresses, rate limits, and what to expect from one server.",
  alternates: { canonical: "/docs/backends" },
};

export default function Backends() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">Guides</p>
      <h1>APIs and backends</h1>
      <p className="text-muted text-lg">
        A backend usually needs more than one public URL: internal services, a database, background
        work, and protection from too much traffic. Here&apos;s how those fit together on Skiff.
      </p>

      <h2>Private services</h2>
      <p>
        Turn on <strong>Private service</strong> under a project&apos;s <strong>Settings → Access</strong>{" "}
        and it stops being reachable from the internet within seconds. The router answers as if it
        doesn&apos;t exist, no certificate is issued for it, and its previews are private too.
      </p>

      <h2>Calling one service from another</h2>
      <p>
        Every project has a stable internal address, shown on its overview. It works from your
        team&apos;s other projects, workers and jobs, stays the same across deploys, and spreads
        calls across replicas:
      </p>
      <CodeBlock label="from another project">{`curl http://billing-api:8080/invoices`}</CodeBlock>
      <p>
        Traffic stays on your team&apos;s private network, and other teams on the server can&apos;t
        reach it. A project needs one deploy after upgrading Skiff for its internal address to work.
        Apps deployed with <code>skiff deploy</code> also need an up-to-date CLI, and their network
        comes from <code>[deploy]</code> in <code>skiff.toml</code>.
      </p>
      <Callout>
        An internal address has no authentication of its own. Anything on your team&apos;s network
        can call it, so keep checking credentials in the service if that matters.
      </Callout>

      <h2>Rate limits</h2>
      <p>
        Set a limit under <strong>Settings → Access</strong>, in requests a minute per visitor. A
        visitor can use a minute&apos;s allowance in one burst, and it refills steadily. Past that
        they get a <code>429</code> with a <code>Retry-After</code> header, before the request
        reaches your app. Calls over internal addresses aren&apos;t counted.
      </p>
      <p>
        A visitor is an IP address, or an IPv6 /64. If you put a CDN like Cloudflare in front of
        Skiff, all its traffic comes from a few addresses and would share one allowance, so set
        limits at the CDN instead.
      </p>

      <h2>gRPC</h2>
      <p>
        gRPC services work with no setting to change, including streaming. Clients connect over HTTPS
        as usual, and Skiff speaks plain-text HTTP/2 to your app, so have the server listen without
        TLS on its port. A service with no web page passes its deploy check by answering over HTTP/2.
        Set the project&apos;s port yourself if the image exposes more than one. gRPC calls count
        toward a rate limit like any other request, and over an internal address they go directly to
        the service.
      </p>

      <h2>The rest of a backend</h2>
      <ul>
        <li>
          <strong>Databases:</strong> managed Postgres, MySQL, MongoDB and Redis, with the connection
          URL added for you, all with <a href="/docs/backups">daily backups</a>.
        </li>
        <li>
          <strong>Migrations:</strong> a release command runs before each new version goes live, and
          a failure stops the deploy.
        </li>
        <li>
          <strong>Background work:</strong> workers and scheduled jobs run from your app&apos;s image.
        </li>
        <li>
          <strong>Real-time:</strong> WebSockets and server-sent events work. When a new version goes
          live, the old one is stopped, so clients should reconnect.
        </li>
      </ul>

      <h2>What Skiff doesn&apos;t do yet</h2>
      <ul>
        <li>
          <strong>Raw TCP or UDP from outside.</strong> The router speaks HTTP and gRPC, and a deploy
          needs an answer in one of them to pass its health check.
        </li>
        <li>
          <strong>More than one server.</strong> Replicas and autoscaling work within your server.
          There&apos;s no failover if it goes down, and no database replicas.
        </li>
      </ul>
    </article>
  );
}
