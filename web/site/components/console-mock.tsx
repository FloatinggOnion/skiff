"use client";

import { useRef } from "react";
import gsap from "gsap";
import { ScrollTrigger } from "gsap/ScrollTrigger";
import { useGSAP } from "@gsap/react";
import { BrowserFrame } from "@/components/browser-frame";

gsap.registerPlugin(ScrollTrigger, useGSAP);

export function ConsoleMock() {
  const scope = useRef<HTMLDivElement>(null);

  useGSAP(
    () => {
      if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
      const tl = gsap.timeline({
        scrollTrigger: { trigger: scope.current, start: "top 78%", once: true },
      });
      tl.from(".log-line", {
        opacity: 0,
        x: -10,
        duration: 0.26,
        stagger: 0.34,
        ease: "power2.out",
      })
        .fromTo(
          ".deploy-bar",
          { scaleX: 0 },
          { scaleX: 1, duration: 1.5, ease: "power1.inOut" },
          0.4,
        )
        .from(".live-line", { opacity: 0, y: 6, duration: 0.4 });
    },
    { scope },
  );

  return (
    <div ref={scope}>
      <BrowserFrame url="app.yourdomain.com" right="skiff" className="glow-amber">
        <div className="space-y-2 p-5 font-mono text-[12.5px] leading-relaxed">
          <p className="log-line text-muted">
            <span className="text-fg">➜</span>{" "}
            <span className="text-fg">git push origin main</span>
          </p>
          <p className="log-line text-subtle">
            GitHub webhook received · cloning
          </p>
          <p className="log-line text-muted">
            → detected <span className="text-fg">Node</span> · installing
            dependencies
          </p>
          <p className="log-line text-muted">
            → building image{" "}
            <span className="text-subtle">skiff-app:latest</span>
          </p>
          <div className="log-line pt-1 pb-0.5">
            <div className="bg-line h-1 w-full overflow-hidden rounded-full">
              <div
                className="deploy-bar h-full origin-left rounded-full"
                style={{ background: "var(--brand)" }}
              />
            </div>
          </div>
          <p className="log-line text-muted">
            → zero-downtime release · health check{" "}
            <span className="text-fg">ok</span>
          </p>
          <p className="live-line text-fg">
            ✓ live ·{" "}
            <span className="text-fg underline underline-offset-2">
              https://app.yourdomain.com
            </span>{" "}
            <span className="text-subtle">(12.9s)</span>
          </p>
        </div>

        <div className="border-line flex items-center justify-between border-t px-5 py-3 font-mono text-[11px]">
          <span className="text-muted flex items-center gap-2">
            <span className="bg-signal animate-pulse-dot h-1.5 w-1.5 rounded-full" />
            running · 1 replica
          </span>
          <span className="text-subtle">HTTPS · auto-renew</span>
        </div>
      </BrowserFrame>
    </div>
  );
}
