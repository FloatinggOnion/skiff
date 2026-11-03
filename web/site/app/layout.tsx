import type { Metadata, Viewport } from "next";
import { Bricolage_Grotesque, Instrument_Sans, JetBrains_Mono } from "next/font/google";
import "./globals.css";

const display = Bricolage_Grotesque({
  variable: "--font-brand-display",
  subsets: ["latin"],
});

const sans = Instrument_Sans({
  variable: "--font-brand-sans",
  subsets: ["latin"],
});

const mono = JetBrains_Mono({
  variable: "--font-brand-mono",
  subsets: ["latin"],
});

const title = "Skiff — Ship it to a server you own";
const description =
  "A self-hosted deployment platform. Build, deploy and manage your apps on your own infrastructure, with automatic HTTPS, managed databases and backups, without piecing together separate tools.";

export const metadata: Metadata = {
  metadataBase: new URL("https://useskiff.xyz"),
  title: {
    default: title,
    template: "%s — Skiff",
  },
  description,
  applicationName: "Skiff",
  category: "technology",
  keywords: [
    "self-hosted deployment platform",
    "push to deploy",
    "deploy to your own server",
    "preview environments",
    "managed databases",
    "automatic HTTPS",
    "Docker deployment",
    "self-hosted PaaS",
    "open source",
  ],
  authors: [{ name: "Skiff", url: "https://useskiff.xyz" }],
  creator: "Skiff",
  publisher: "Skiff",
  alternates: {
    canonical: "/",
  },
  robots: {
    index: true,
    follow: true,
    googleBot: {
      index: true,
      follow: true,
      "max-image-preview": "large",
      "max-snippet": -1,
      "max-video-preview": -1,
    },
  },
  openGraph: {
    title,
    description,
    url: "https://useskiff.xyz",
    siteName: "Skiff",
    type: "website",
    locale: "en_US",
  },
  twitter: {
    card: "summary_large_image",
    title,
    description,
  },
};

export const viewport: Viewport = {
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#f6f8f5" },
    { media: "(prefers-color-scheme: dark)", color: "#142c29" },
  ],
  colorScheme: "dark",
};

const jsonLd = {
  "@context": "https://schema.org",
  "@type": "SoftwareApplication",
  name: "Skiff",
  applicationCategory: "DeveloperApplication",
  operatingSystem: "Linux, macOS",
  description,
  url: "https://useskiff.xyz",
  isAccessibleForFree: true,
  license: "https://opensource.org/license/mit",
  offers: {
    "@type": "Offer",
    price: "0",
    priceCurrency: "USD",
  },
  author: {
    "@type": "Organization",
    name: "Skiff",
    url: "https://useskiff.xyz",
  },
};

// Runs before first paint, so a dark-theme reload never flashes light.
const THEME_INIT = `try{var t=localStorage.getItem('skiff-theme');if(t==='dark'||(t!=='light'&&matchMedia('(prefers-color-scheme: dark)').matches))document.documentElement.classList.add('dark')}catch(e){}`;

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode;
}>) {
  return (
    <html
      lang="en"
      suppressHydrationWarning
      className={`${display.variable} ${sans.variable} ${mono.variable} antialiased`}
    >
      <body>
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT }} />
        <script
          type="application/ld+json"
          dangerouslySetInnerHTML={{ __html: JSON.stringify(jsonLd) }}
        />
        {children}
      </body>
    </html>
  );
}
