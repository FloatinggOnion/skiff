import type { Metadata } from "next";
import Link from "next/link";
import { ArrowRight, Server, Terminal } from "lucide-react";

export const metadata: Metadata = {
  title: "Docs",
  description: "Run Skiff on a server you own: push-to-deploy, automatic HTTPS, and preview environments.",
  alternates: { canonical: "/docs" },
};

export default function DocsHome() {
  return (
    <article className="prose prose-invert max-w-none">
      <p className="not-prose text-brand text-sm font-medium">Docs</p>
      <h1>Ship to a server you own</h1>
      <p className="text-muted text-lg">
        Skiff turns a plain Linux box into your own deploy platform — push-to-deploy, automatic
        HTTPS, instant rollbacks, and preview environments, on infrastructure you control.
      </p>

      <div className="not-prose mt-10 grid gap-4 sm:grid-cols-2">
        <Link
          href="/docs/self-hosting"
          className="border-line bg-surface hover:border-line-strong group rounded-xl border p-5 transition"
        >
          <Server className="text-fg h-5 w-5" />
          <h2 className="font-display text-fg mt-3 flex items-center gap-1.5 font-semibold">
            Self-hosting
            <ArrowRight className="h-4 w-4 opacity-0 transition group-hover:translate-x-0.5 group-hover:opacity-60" />
          </h2>
          <p className="text-muted mt-1 text-sm leading-relaxed">
            One command to stand up the dashboard + router on your server, with auto-HTTPS.
          </p>
        </Link>

        <div className="border-line bg-surface rounded-xl border p-5">
          <Terminal className="text-fg h-5 w-5" />
          <h2 className="font-display text-fg mt-3 font-semibold">From the CLI</h2>
          <p className="text-muted mt-1 text-sm leading-relaxed">
            Prefer the terminal? <code className="font-mono">skiff deploy</code> builds and runs any
            app on local Docker or a remote box over SSH.
          </p>
        </div>
      </div>

      <p>
        New here? Start with{" "}
        <Link href="/docs/self-hosting">Self-hosting</Link> — it takes a fresh server to a working
        dashboard in a few minutes.
      </p>
    </article>
  );
}
