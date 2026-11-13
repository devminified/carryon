// CO-702: the approved mark from the visual-direction mockup — a
// luggage tag whose own string-hole is cut as a play/resume triangle.
// One flat <path>, fill-rule="evenodd", fill="currentColor" — checked
// legible from 220px down to 18px before this shape was approved, so
// it holds up at the sizes this component actually renders at (a
// small nav tile and a favicon-adjacent size), not just in isolation.
export function LogoMark({ className }: { className?: string }) {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true" className={className}>
      <path
        fillRule="evenodd"
        clipRule="evenodd"
        fill="currentColor"
        d="M2.5 5 H14.5 L21.5 12 L14.5 19 H2.5 Z M14.2 9.1 L18.6 12 L14.2 14.9 Z"
      />
    </svg>
  );
}

export function Wordmark({ className }: { className?: string }) {
  return (
    <span className={`inline-flex items-center gap-2 ${className ?? ''}`}>
      <span className="bg-primary text-primary-foreground flex size-6 shrink-0 items-center justify-center rounded-md">
        <LogoMark className="size-3.5" />
      </span>
      <span className="font-semibold">Carryon</span>
    </span>
  );
}
