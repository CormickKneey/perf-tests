export function BrandMark({ className = "brand-mark" }: { className?: string }) {
  // Four wings around a body: a plain geometric dragonfly, no third-party logo.
  return (
    <svg className={className} viewBox="0 0 16 16" aria-hidden="true">
      <rect x="1" y="3" width="5" height="3" fill="currentColor" />
      <rect x="10" y="3" width="5" height="3" fill="currentColor" />
      <rect x="2" y="8" width="4" height="2" fill="currentColor" opacity="0.55" />
      <rect x="10" y="8" width="4" height="2" fill="currentColor" opacity="0.55" />
      <rect x="7" y="2" width="2" height="12" style={{ fill: "var(--orange-fire)" }} />
    </svg>
  );
}

export function Verdict({ verdict, label }: { verdict: string; label?: string }) {
  return <span className={`verdict verdict-${verdict}`}>{label ?? verdict}</span>;
}
