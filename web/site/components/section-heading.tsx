export function SectionHeading({
  eyebrow,
  title,
  className = "",
}: {
  eyebrow: string;
  title: string;
  className?: string;
}) {
  return (
    <div className={"max-w-2xl " + className}>
      <p className="text-brand text-sm font-medium">
        {eyebrow}
      </p>
      <h2 className="font-display mt-4 text-3xl font-semibold tracking-tight text-balance sm:text-4xl">
        {title}
      </h2>
    </div>
  );
}
