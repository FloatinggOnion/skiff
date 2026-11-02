import { BrowserFrame } from "@/components/browser-frame";
import { Logo } from "@/components/logo";

const GROUPS = [
  { label: "Ship", items: ["Projects", "Deployments"] },
  { label: "Run", items: ["Databases", "Storage", "Domains", "Email"] },
  { label: "Watch", items: ["Logs", "Analytics", "Activity"] },
];

const FACTS = [
  ["Address", "shop-api.example.com"],
  ["Live version", "a41f9c2"],
  ["Last deploy", "Succeeded 12m ago"],
  ["Replicas", "2 running"],
];

const LOG = [
  { t: "→ cloning acme/shop-api (main)", c: "text-muted" },
  { t: "→ Building skiff-shop-api:latest", c: "text-muted" },
  { t: "✓ Built in 41s", c: "text-signal" },
  { t: "→ Starting new version · 2 replicas", c: "text-muted" },
  { t: "✓ Healthy", c: "text-signal" },
  { t: "✓ Live at https://shop-api.example.com", c: "text-signal" },
];

export function DashboardPreview() {
  return (
    <BrowserFrame url="dash.example.com" className="glow-amber">
      <div className="grid text-sm md:grid-cols-[190px_1fr]">
        <aside className="border-line hidden flex-col gap-1 border-r p-4 md:flex">
          <Logo className="mb-3" />
          {GROUPS.map((g) => (
            <div key={g.label} className="mb-2">
              <p className="text-subtle mb-1 px-2 font-mono text-[10px] tracking-wider uppercase">
                {g.label}
              </p>
              {g.items.map((i) => (
                <p
                  key={i}
                  className={
                    "rounded-md px-2 py-1 " +
                    (i === "Projects" ? "bg-elevated text-fg" : "text-muted")
                  }
                >
                  {i}
                </p>
              ))}
            </div>
          ))}
          <div className="mt-auto grid gap-1 px-2 pt-3 font-mono text-[10px]">
            <span className="text-muted">Your server · disk 62%</span>
            <span className="bg-line h-1 overflow-hidden rounded-full">
              <span className="bg-brand block h-full w-[62%]" />
            </span>
          </div>
        </aside>
        <div className="flex min-w-0 flex-col">
          <div className="p-5">
            <div className="flex flex-wrap items-center gap-3">
              <p className="font-display text-xl font-bold">shop-api</p>
              <span className="text-signal font-mono text-[11px]">● running</span>
              <span className="btn-brand ml-auto rounded-md px-3 py-1.5 text-xs font-semibold">
                Redeploy
              </span>
            </div>
            <dl className="mt-5 grid grid-cols-2 gap-4 lg:grid-cols-4">
              {FACTS.map(([k, v]) => (
                <div key={k} className="min-w-0">
                  <dt className="text-subtle font-mono text-[10px] tracking-wider uppercase">{k}</dt>
                  <dd className="mt-1 truncate font-mono text-xs">{v}</dd>
                </div>
              ))}
            </dl>
          </div>
          <div className="well flex-1 overflow-x-auto px-5 py-4 font-mono text-xs leading-relaxed">
            {LOG.map((l) => (
              <p key={l.t} className={"whitespace-pre " + l.c}>
                {l.t}
              </p>
            ))}
          </div>
        </div>
      </div>
    </BrowserFrame>
  );
}
