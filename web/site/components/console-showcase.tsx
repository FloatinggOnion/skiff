import { DashboardPreview } from "@/components/dashboard-preview";

export function ConsoleShowcase() {
  return (
    <section className="mx-auto max-w-6xl px-6 py-24">
      <div className="max-w-2xl">
        <h2 className="font-display text-3xl font-bold tracking-tight text-balance sm:text-4xl">
          Your server gets a console.
        </h2>
        <p className="text-muted mt-4 text-lg leading-relaxed">
          Above the line, what&apos;s live. Below it, the work that got it there. Deploy, roll back,
          open a shell and manage every app, database and domain from one place.
        </p>
      </div>
      <div className="mt-12">
        <DashboardPreview />
      </div>
    </section>
  );
}
