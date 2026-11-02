import { Logo } from "@/components/logo";
import { GitHubStars } from "@/components/github-star";
import { ThemeToggle } from "@/components/theme-toggle";

const links = [
  { href: "/#why", label: "Why Skiff" },
  { href: "/#features", label: "Features" },
  { href: "/#how", label: "How it works" },
  { href: "/docs", label: "Docs" },
];

export function SiteNav() {
  return (
    <header className="border-line/60 bg-bg/80 sticky top-0 z-50 border-b backdrop-blur-xl">
      <div className="mx-auto flex h-16 max-w-6xl items-center justify-between px-6">
        <a href="/" aria-label="Skiff home">
          <Logo />
        </a>
        <nav className="text-muted hidden items-center gap-8 text-sm md:flex">
          {links.map((l) => (
            <a key={l.href} href={l.href} className="hover:text-fg transition-colors">
              {l.label}
            </a>
          ))}
        </nav>
        <div className="flex items-center gap-2">
          <span className="hidden sm:block">
            <GitHubStars />
          </span>
          <ThemeToggle />
          <a href="/docs" className="btn-brand rounded-lg px-4 py-2 text-sm font-semibold">
            Get started
          </a>
        </div>
      </div>
    </header>
  );
}
