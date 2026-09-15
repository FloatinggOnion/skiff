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
    items: [{ href: "/docs/email-forwarding", label: "Email forwarding" }],
  },
];

// Flat, ordered list for previous/next page navigation.
export const DOCS_FLAT = DOCS_NAV.flatMap((s) => s.items);
