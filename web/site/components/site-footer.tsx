import { Logo } from "@/components/logo";
import { GITHUB_URL } from "@/lib/site";

export function SiteFooter() {
  return (
    <footer className="border-line border-t">
      <div className="mx-auto flex max-w-6xl flex-col gap-6 px-6 py-10 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <Logo />
          <p className="text-muted mt-2 text-sm">
            Small boat, your own water.
          </p>
        </div>
        <div className="text-subtle flex flex-wrap items-center gap-6 text-sm">
          <a href="/docs" className="hover:text-fg transition-colors">
            Docs
          </a>
          <a href="/#features" className="hover:text-fg transition-colors">
            Features
          </a>
          <a
            href={GITHUB_URL}
            target="_blank"
            rel="noreferrer"
            className="hover:text-fg transition-colors"
          >
            GitHub
          </a>
          <span className="text-subtle/70">&copy; {new Date().getFullYear()} Skiff</span>
        </div>
      </div>
    </footer>
  );
}
