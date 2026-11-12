import { Fragment } from 'react';

const REDACTED_RE = /\[REDACTED:[^\]]+\]/g;

// Splits on the laptop/server's own "[REDACTED:<rule-id>]" placeholder
// (internal/redact, redaction.md) and highlights it — plain React
// elements, never dangerouslySetInnerHTML (CLAUDE.md: no raw HTML
// rendering of transcript content).
export function HighlightedText({ text }: { text: string }) {
  const parts = text.split(REDACTED_RE);
  const matches = [...text.matchAll(REDACTED_RE)].map((m) => m[0]);

  return (
    <>
      {parts.map((part, i) => (
        <Fragment key={i}>
          {part}
          {i < matches.length && (
            <mark className="bg-warning/15 text-warning dark:bg-warning/25 rounded px-1">
              {matches[i]}
            </mark>
          )}
        </Fragment>
      ))}
    </>
  );
}

export function HighlightedPre({ text }: { text: string }) {
  return (
    <pre className="bg-border/50 overflow-x-auto rounded p-2 text-xs whitespace-pre-wrap">
      <HighlightedText text={text} />
    </pre>
  );
}
