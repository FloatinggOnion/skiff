import type { Metadata } from 'next';
import { RootProvider } from 'fumadocs-ui/provider/next';
import { Bricolage_Grotesque, Instrument_Sans, JetBrains_Mono } from 'next/font/google';
import { docsUrl } from '@/lib/shared';
import './global.css';

const display = Bricolage_Grotesque({ variable: '--font-brand-display', subsets: ['latin'] });
const sans = Instrument_Sans({ variable: '--font-brand-sans', subsets: ['latin'] });
const mono = JetBrains_Mono({ variable: '--font-brand-mono', subsets: ['latin'] });

export const metadata: Metadata = {
  metadataBase: new URL(docsUrl),
  title: { default: 'Skiff docs', template: '%s — Skiff docs' },
  description: 'How to deploy and run apps, databases and whole Compose stacks on a server you own with Skiff.',
  applicationName: 'Skiff',
  icons: { icon: '/icon.svg', apple: '/apple-icon.png' },
};

export default function Layout({ children }: LayoutProps<'/'>) {
  return (
    <html
      lang="en"
      className={`${display.variable} ${sans.variable} ${mono.variable} font-sans`}
      suppressHydrationWarning
    >
      <body className="flex min-h-screen flex-col">
        <RootProvider>{children}</RootProvider>
      </body>
    </html>
  );
}
