const reasons = [
  {
    title: "The machine is yours",
    body: "Any Linux server from any provider, at its flat monthly price. Skiff shows you its disk and memory, and what each app costs out of it.",
  },
  {
    title: "Small enough to understand",
    body: "One Go binary and plain Docker containers. No cluster, no YAML, no control plane to run. If you know what a server is, you know how this works.",
  },
  {
    title: "Nothing to leave behind",
    body: "Open source, under MIT or Apache 2.0. Your apps are ordinary containers and your data sits on your disk, so you can walk away whenever you like.",
  },
];

export function WhySkiff() {
  return (
    <section id="why" className="mx-auto max-w-6xl scroll-mt-20 px-6 py-24">
      <div className="max-w-2xl">
        <h2 className="font-display text-3xl font-bold tracking-tight text-balance sm:text-5xl">
          Most apps don&apos;t need a container ship.
        </h2>
        <p className="text-muted mt-5 text-lg leading-relaxed">
          The tools around deployment are built for fleets. A skiff is a small boat one person can
          handle and own outright, and most of what you ship fits in one.
        </p>
      </div>
      <div className="mt-14 grid gap-10 md:grid-cols-3">
        {reasons.map((r) => (
          <div key={r.title}>
            <h3 className="font-display text-lg font-bold">{r.title}</h3>
            <p className="text-muted mt-2 leading-relaxed">{r.body}</p>
          </div>
        ))}
      </div>
    </section>
  );
}
