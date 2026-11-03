export type DocLink = { href: string; label: string };
export type DocSection = { title: string; items: DocLink[] };

export const DOCS_NAV: DocSection[] = [
  {
    title: "Getting started",
    items: [
      { href: "/docs", label: "Overview" },
      { href: "/docs/self-hosting", label: "Self-hosting" },
    ],
  },
  {
    title: "Guides",
    items: [
      { href: "/docs/backends", label: "APIs and backends" },
      { href: "/docs/compose", label: "Docker Compose" },
      { href: "/docs/previews", label: "Preview environments" },
      { href: "/docs/one-click-apps", label: "One-click apps" },
      { href: "/docs/images", label: "Prebuilt images" },
      { href: "/docs/volumes", label: "Persistent volumes" },
      { href: "/docs/backups", label: "Backups and encryption" },
      { href: "/docs/log-drains", label: "Log drains" },
      { href: "/docs/status-pages", label: "Status pages" },
      { href: "/docs/email-forwarding", label: "Email forwarding" },
      { href: "/docs/github-actions", label: "GitHub Actions" },
      { href: "/docs/agents", label: "AI agents (MCP)" },
    ],
  },
  {
    title: "Reference",
    items: [{ href: "/docs/api", label: "API" }],
  },
];

// Flat, ordered list for previous/next page navigation.
export const DOCS_FLAT = DOCS_NAV.flatMap((s) => s.items);
