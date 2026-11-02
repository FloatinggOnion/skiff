import { Check } from "lucide-react";
import type { ReactNode } from "react";
import { ConsoleMock } from "@/components/console-mock";

function PreviewVisual() {
  const branches = [
    { b: "main", u: "app.yourdomain.com" },
    { b: "feat-auth", u: "feat-auth.yourdomain.com" },
    { b: "fix-cache", u: "fix-cache.yourdomain.com" },
  ];
  return (
    <div className="border-line bg-surface/70 glow-amber space-y-2.5 rounded-2xl border p-5">
      {branches.map((x) => (
        <div
          key={x.b}
          className="border-line bg-bg/50 flex items-center justify-between rounded-lg border px-4 py-3"
        >
          <span className="text-fg font-mono text-xs">{x.b}</span>
          <span className="text-subtle flex items-center gap-2 font-mono text-[11px]">
            {x.u}
            <span className="bg-signal h-1.5 w-1.5 rounded-full" />
          </span>
        </div>
      ))}
    </div>
  );
}

function RolloutVisual() {
  return (
    <div className="border-line bg-surface/70 glow-amber rounded-2xl border p-6 font-mono text-xs">
      <div className="flex items-center justify-between">
        <span className="text-subtle">v41 → v42</span>
        <span className="text-signal flex items-center gap-1.5">
          <span className="bg-signal h-1.5 w-1.5 rounded-full" /> healthy
        </span>
      </div>
      <div className="bg-line mt-4 h-1.5 w-full overflow-hidden rounded-full">
        <div
          className="h-full w-full rounded-full"
          style={{ background: "var(--brand)" }}
        />
      </div>
      <div className="text-subtle mt-4 grid grid-cols-2 gap-3">
        <span>GET /healthz · 200</span>
        <span className="text-right">2 of 2 replicas</span>
        <span>traffic · moved</span>
        <span className="text-right">v41 · retired</span>
      </div>
    </div>
  );
}

function DatabaseVisual() {
  const engines = ["postgres", "mysql", "mongodb", "redis"];
  return (
    <div className="border-line bg-surface/70 glow-amber space-y-3 rounded-2xl border p-5">
      <div className="grid grid-cols-2 gap-2.5">
        {engines.map((e) => (
          <div
            key={e}
            className="border-line bg-bg/50 flex items-center justify-between rounded-lg border px-3.5 py-2.5"
          >
            <span className="text-fg font-mono text-xs">{e}</span>
            <span className="bg-signal h-1.5 w-1.5 rounded-full" />
          </div>
        ))}
      </div>
      <div className="border-line bg-bg/50 flex items-center gap-2.5 overflow-hidden rounded-lg border px-3.5 py-2.5">
        <span className="text-brand shrink-0 font-mono text-[11px]">DATABASE_URL</span>
        <span className="text-subtle truncate font-mono text-[11px]">
          postgres://app@db:5432/app
        </span>
      </div>
    </div>
  );
}

function AnalyticsVisual() {
  const bars = [38, 55, 48, 66, 80, 62, 74, 52, 68, 88, 60, 78, 46, 70];
  return (
    <div className="border-line bg-surface/70 glow-amber rounded-2xl border p-6">
      <div className="flex items-baseline justify-between">
        <span className="font-display text-fg text-2xl font-semibold tracking-tight">
          9,143
        </span>
        <span className="text-subtle font-mono text-[11px]">edge requests · 1h</span>
      </div>
      <div className="mt-5 flex h-24 items-end gap-1.5">
        {bars.map((h, i) => (
          <div
            key={i}
            className="flex-1 rounded-sm"
            style={{
              height: `${h}%`,
              background: "var(--brand)",
            }}
          />
        ))}
      </div>
      <div className="border-line text-subtle mt-4 grid grid-cols-3 gap-2 border-t pt-3 font-mono text-[11px]">
        <span>2XX · 98.6%</span>
        <span className="text-center">p50 · 41ms</span>
        <span className="text-right">32.7MB out</span>
      </div>
    </div>
  );
}

function Row({
  eyebrow,
  title,
  body,
  points,
  visual,
  flip = false,
}: {
  eyebrow: string;
  title: string;
  body: string;
  points: string[];
  visual: ReactNode;
  flip?: boolean;
}) {
  return (
    <div className="grid items-center gap-10 lg:grid-cols-2 lg:gap-16">
      <div className={flip ? "lg:order-2" : ""}>
        <p className="text-brand text-sm font-medium">
          {eyebrow}
        </p>
        <h3 className="font-display mt-4 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
          {title}
        </h3>
        <p className="text-muted mt-4 max-w-md leading-relaxed">{body}</p>
        <ul className="mt-6 space-y-2.5">
          {points.map((p) => (
            <li key={p} className="text-muted flex items-start gap-2.5 text-sm">
              <Check className="text-signal mt-0.5 h-4 w-4 shrink-0" strokeWidth={2.5} />
              {p}
            </li>
          ))}
        </ul>
      </div>
      <div className={flip ? "lg:order-1" : ""}>{visual}</div>
    </div>
  );
}

export function FeatureRows() {
  return (
    <section id="features" className="mx-auto max-w-6xl px-6 py-28">
      <div className="space-y-28">
        <Row
          eyebrow="Deploy"
          title="Push, and it's live."
          body="Point Skiff at a repo and it detects the stack, builds an image, and runs it — no Dockerfile, no YAML, no pipeline to babysit."
          points={[
            "Git push or one click in the console",
            "Automatic HTTPS on your own domain",
            "Ten-plus languages detected, or bring a Dockerfile or a prebuilt image",
          ]}
          visual={<ConsoleMock />}
        />
        <Row
          flip
          eyebrow="Databases"
          title="A database, one click away."
          body="Provision Postgres, MySQL, MongoDB, or Redis right beside your app. Skiff runs it on a private network, injects the connection URL, and gives you a shell — no separate provider, no egress bill."
          points={[
            "Postgres, MySQL, MongoDB, and Redis",
            "Attach to an app and the connection URL is injected",
            "A browser shell into every database",
          ]}
          visual={<DatabaseVisual />}
        />
        <Row
          eyebrow="Previews"
          title="Every branch gets a URL."
          body="Open a branch and Skiff spins up an isolated environment at its own address, so you can review real changes before they reach production."
          points={[
            "Isolated per-branch environments",
            "Shareable preview links",
            "Torn down when the branch is gone",
          ]}
          visual={<PreviewVisual />}
        />
        <Row
          flip
          eyebrow="Observability"
          title="See every request."
          body="The edge router meters everything it serves — status codes, data transfer, latency — and charts it live, per app. No agent, no extra service."
          points={[
            "Requests, latency, and transfer over time",
            "Filter by app and time range",
            "Live from the edge — nothing to install",
          ]}
          visual={<AnalyticsVisual />}
        />
        <Row
          eyebrow="Reliability"
          title="A bad build never takes traffic."
          body="A new version has to pass its health check before traffic moves to it. If it never does, the deploy stops and the running version keeps serving."
          points={[
            "Set your own health check path and timeout",
            "Release commands, like migrations, run before the swap",
            "Instant rollback to any of the last five builds",
          ]}
          visual={<RolloutVisual />}
        />
      </div>
    </section>
  );
}
