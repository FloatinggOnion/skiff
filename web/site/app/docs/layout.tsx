import type { ReactNode } from "react";
import { SiteNav } from "@/components/site-nav";
import { SiteFooter } from "@/components/site-footer";
import { DocsSidebar } from "@/components/docs-sidebar";
import { DocsToc } from "@/components/docs-toc";
import { DocsPager } from "@/components/docs-pager";

export default function DocsLayout({ children }: { children: ReactNode }) {
  return (
    <>
      <SiteNav />
      <div className="mx-auto flex max-w-6xl gap-10 px-6 py-12">
        <DocsSidebar />
        <main className="min-w-0 flex-1">
          {children}
          <DocsPager />
        </main>
        <DocsToc />
      </div>
      <SiteFooter />
    </>
  );
}
