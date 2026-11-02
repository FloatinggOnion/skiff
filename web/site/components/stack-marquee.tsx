import type { IconType } from "react-icons";
import {
  SiNodedotjs,
  SiPython,
  SiGo,
  SiRust,
  SiRuby,
  SiElixir,
  SiPhp,
  SiDotnet,
  SiNextdotjs,
  SiVite,
  SiHtml5,
} from "react-icons/si";
import { FaJava } from "react-icons/fa";

const stacks: { Icon: IconType; label: string }[] = [
  { Icon: SiNodedotjs, label: "Node.js" },
  { Icon: SiPython, label: "Python" },
  { Icon: SiGo, label: "Go" },
  { Icon: SiRust, label: "Rust" },
  { Icon: SiRuby, label: "Ruby" },
  { Icon: SiElixir, label: "Elixir" },
  { Icon: FaJava, label: "Java" },
  { Icon: SiDotnet, label: ".NET" },
  { Icon: SiPhp, label: "PHP" },
  { Icon: SiHtml5, label: "Static" },
  { Icon: SiNextdotjs, label: "Next.js" },
  { Icon: SiVite, label: "Vite" },
];

// Repeat enough that half the track (the -50% travel) always exceeds the
// widest screens, so the loop is seamless and the row is never empty.
const items = Array.from({ length: 8 }, () => stacks).flat();

function Row({ reverse = false }: { reverse?: boolean }) {
  return (
    <div
      className={
        (reverse ? "marquee-track-rev" : "marquee-track") +
        " flex w-max items-center gap-14"
      }
    >
      {items.map((s, i) => (
        <span
          key={i}
          title={s.label}
          aria-label={s.label}
          className="text-fg/45 hover:text-fg shrink-0 transition-colors"
        >
          <s.Icon className="h-8 w-8" />
        </span>
      ))}
    </div>
  );
}

export function StackMarquee() {
  return (
    <section id="stacks" className="border-line/60 border-y py-12">
      <p className="text-muted mb-8 text-center text-sm">
        Speaks every stack — no Dockerfile required
      </p>
      <div className="relative space-y-6 overflow-hidden [mask-image:linear-gradient(to_right,transparent,#000_8%,#000_92%,transparent)]">
        <Row />
        <Row reverse />
      </div>
    </section>
  );
}
