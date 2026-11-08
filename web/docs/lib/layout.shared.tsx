import type { BaseLayoutProps } from 'fumadocs-ui/layouts/shared';
import { Logo } from '@/components/logo';
import { gitConfig, siteUrl } from './shared';

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      title: (
        <span className="inline-flex items-center gap-2">
          <Logo />
          <span className="text-fd-muted-foreground border-fd-border rounded-md border px-1.5 py-0.5 font-mono text-[10px] tracking-[0.18em] uppercase">
            Docs
          </span>
        </span>
      ),
      url: '/',
    },
    links: [{ text: 'useskiff.xyz', url: siteUrl, external: true }],
    githubUrl: `https://github.com/${gitConfig.user}/${gitConfig.repo}`,
  };
}
