"use client";

import { useRef } from "react";
import gsap from "gsap";
import { useGSAP } from "@gsap/react";
import { ArrowRight, Copy } from "lucide-react";
import { LogoMark } from "@/components/logo";

gsap.registerPlugin(useGSAP);

const INSTALL = "curl -fsSL useskiff.xyz/install | sh -s -- --domain example.com";

const LOG: { text: string; tone?: string }[] = [
  { text: "$ skiff deploy", tone: "text-fg" },
  { text: "  Deploying shop-api" },
  { text: "  target   root@your-server" },
  { text: "  build    Node.js" },
  { text: "  → Building skiff-shop-api:latest", tone: "text-[#8ccbe8]" },
  { text: "  ✓ Built skiff-shop-api:latest", tone: "text-signal" },
  { text: "  → Starting new version · 2 replicas", tone: "text-[#8ccbe8]" },
  { text: "  ✓ Healthy", tone: "text-signal" },
  { text: "  ✓ Retired the previous version", tone: "text-signal" },
  { text: "  ✓ Live at https://shop-api.example.com", tone: "text-signal" },
];

export function Hero() {
  const scope = useRef<HTMLElement>(null);

  useGSAP(
    () => {
      if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
      gsap.from(".hero-item", { opacity: 0, y: 18, duration: 0.8, stagger: 0.08, ease: "power3.out" });
      gsap.from(".hero-hull", { y: 70, duration: 1.3, ease: "power3.out" });
      gsap.from(".log-row", { opacity: 0, duration: 0.2, stagger: 0.16, delay: 0.5, ease: "none" });
    },
    { scope },
  );

  return (
    <section ref={scope}>
      {/* Above the waterline: the promise, and a hull that sits in the water below it. */}
      <div className="relative overflow-hidden">
        <div className="hero-hull text-hull pointer-events-none absolute right-[-5rem] -bottom-40 hidden lg:block xl:right-[6%]">
          <LogoMark className="!text-hull h-[640px] w-[640px]" />
        </div>
        <div className="relative mx-auto max-w-6xl px-6 pt-20 pb-16 lg:pt-28 lg:pb-24">
          <p className="hero-item text-brand text-sm font-medium">Small boat, your own water.</p>
          <h1 className="hero-item font-display mt-5 max-w-2xl text-5xl leading-[0.98] font-bold tracking-tight text-balance sm:text-6xl md:text-[5rem]">
            Ship it to a server you own.
          </h1>
          <p className="hero-item text-muted mt-6 max-w-md text-lg leading-relaxed">
            A self-hosted deployment platform. Build, deploy and manage your apps on your own
            infrastructure, without piecing together separate tools.
          </p>
          <div className="hero-item mt-8 flex flex-wrap items-center gap-3">
            <a href="/docs" className="btn-brand rounded-lg px-5 py-2.5 text-sm font-semibold">
              Get started
            </a>
            <a
              href="#how"
              className="group border-line-strong bg-bg/60 text-fg hover:bg-elevated inline-flex items-center gap-1.5 rounded-lg border px-5 py-2.5 text-sm font-medium transition-colors"
            >
              See how it works
              <ArrowRight className="h-4 w-4 transition-transform group-hover:translate-x-0.5" />
            </a>
          </div>
        </div>
      </div>

      {/* Below it: what actually happens. Always dark, in either theme. */}
      <div className="well relative">
        <div className="mx-auto grid max-w-6xl grid-cols-1 gap-10 px-6 py-12 lg:grid-cols-[1fr_1.25fr] lg:items-center">
          <div className="min-w-0">
            <h2 className="font-display text-2xl font-bold tracking-tight">
              One command on a fresh server.
            </h2>
            <p className="text-muted mt-3 max-w-sm leading-relaxed">
              It sets up Docker, the edge router and the web console on any Ubuntu or Debian box.
            </p>
            <div className="border-line bg-surface text-muted mt-6 flex items-center justify-between gap-3 rounded-lg border px-4 py-2.5 font-mono text-[13px]">
              <span className="truncate">
                <span className="text-brand">$</span> {INSTALL}
              </span>
              <button
                type="button"
                aria-label="Copy install command"
                onClick={() => navigator.clipboard?.writeText(INSTALL)}
                className="text-subtle hover:text-fg shrink-0 cursor-pointer transition-colors"
              >
                <Copy className="h-3.5 w-3.5" />
              </button>
            </div>
          </div>
          <div className="min-w-0 overflow-x-auto font-mono text-[13px] leading-[1.7]">
            {LOG.map((l) => (
              <div key={l.text} className={"log-row whitespace-pre " + (l.tone ?? "text-muted")}>
                {l.text}
              </div>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}
