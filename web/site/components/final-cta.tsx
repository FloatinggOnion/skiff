export function FinalCta() {
  return (
    <section className="well">
      <div className="mx-auto max-w-6xl px-6 py-20">
        <h2 className="font-display max-w-xl text-4xl font-bold tracking-tight text-balance sm:text-5xl">
          Your code, your server, one command.
        </h2>
        <p className="text-muted mt-4 max-w-md text-lg">Free and open source, on any Linux box you have.</p>
        <div className="mt-8 flex flex-wrap items-center gap-3">
          <a href="/docs" className="btn-brand rounded-lg px-5 py-2.5 text-sm font-semibold">
            Get started
          </a>
          <a
            href="/docs/self-hosting"
            className="border-line-strong text-fg hover:bg-elevated rounded-lg border px-5 py-2.5 text-sm font-medium transition-colors"
          >
            Read the setup guide
          </a>
        </div>
      </div>
    </section>
  );
}
