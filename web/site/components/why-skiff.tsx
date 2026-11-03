const reasons = [
  {
    title: "Simpler deployment",
    body: "Push to Git or run one command. Skiff detects your stack, builds it with Docker and switches to the new version once it's healthy. Most stacks don't need a Dockerfile.",
  },
  {
    title: "Less infrastructure overhead",
    body: "Automatic HTTPS, domain routing, managed databases, backups and a control panel all come from one Go binary. There's no cluster to run and no stack of tools to wire together.",
  },
  {
    title: "More control",
    body: "Any Linux server from any provider, at its flat monthly price. It's open source, your apps are ordinary containers and your data sits on your disk, so you can walk away whenever you like.",
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
