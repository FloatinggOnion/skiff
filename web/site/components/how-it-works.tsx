import { SectionHeading } from "@/components/section-heading";

const steps = [
  {
    n: "01",
    title: "Set up your server",
    body: "One command on a fresh Ubuntu or Debian box installs everything.",
  },
  {
    n: "02",
    title: "Point it at your code",
    body: "Connect a GitHub repo, or use a Git URL, an image or the CLI.",
  },
  {
    n: "03",
    title: "Get an HTTPS URL",
    body: "Skiff builds it, runs it and serves it on your domain.",
  },
];

export function HowItWorks() {
  return (
    <section id="how" className="mx-auto max-w-6xl px-6 py-24">
      <SectionHeading
        eyebrow="How it works"
        title="From zero to deployed in three moves."
      />
      <div className="mt-14 grid gap-10 sm:grid-cols-3">
        {steps.map((s) => (
          <div key={s.n}>
            <div className="flex items-center gap-3">
              <span className="font-display text-brand-2 text-2xl font-bold">
                {s.n}
              </span>
              <span className="from-line-strong h-px flex-1 bg-gradient-to-r to-transparent" />
            </div>
            <h3 className="font-display mt-5 text-lg font-semibold">{s.title}</h3>
            <p className="text-muted mt-2 text-sm leading-relaxed">{s.body}</p>
          </div>
        ))}
      </div>
    </section>
  );
}
