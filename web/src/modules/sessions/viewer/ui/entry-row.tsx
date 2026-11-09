import type { ContentBlock, TranscriptEntry } from '../lib/parse-transcript';
import { MarkdownText } from './markdown-text';
import { ToolCall } from './tool-call';

type ToolResultBlock = Extract<ContentBlock, { type: 'tool_result' }>;

function Block({
  block,
  toolResults,
}: {
  block: ContentBlock;
  toolResults: Map<string, ToolResultBlock>;
}) {
  switch (block.type) {
    case 'text':
      return <MarkdownText text={block.text} />;
    case 'tool_use':
      return <ToolCall toolUse={block} result={toolResults.get(block.id) ?? null} />;
    case 'tool_result':
      // Only reachable for a *mixed* message (real text alongside a
      // result, see correlate.ts) — a pure-result message is filtered
      // out of the visible list entirely, its content already shown
      // via the paired ToolCall above.
      return null;
    case 'unknown':
      return (
        <pre className="bg-border/50 text-muted-foreground overflow-x-auto rounded p-2 text-xs">
          {JSON.stringify(block.raw, null, 2)}
        </pre>
      );
  }
}

export function EntryRow({
  entry,
  toolResults,
}: {
  entry: TranscriptEntry;
  toolResults: Map<string, ToolResultBlock>;
}) {
  if (entry.kind === 'summary') {
    return (
      <div className="border-border text-muted-foreground my-2 rounded border border-dashed px-3 py-1.5 text-xs italic">
        {entry.summary}
      </div>
    );
  }

  if (entry.kind === 'unknown') {
    return (
      <div className="my-2">
        <p className="text-muted-foreground mb-1 text-xs">
          Unrecognized line — shown as raw JSON so a future Claude Code format degrades gracefully
          instead of breaking the viewer.
        </p>
        <pre className="bg-border/50 overflow-x-auto rounded p-2 text-xs">
          {JSON.stringify(entry.raw, null, 2)}
        </pre>
      </div>
    );
  }

  const align = entry.role === 'user' ? 'items-end' : 'items-start';
  return (
    <div className={`my-3 flex flex-col ${align}`}>
      <span className="text-muted-foreground mb-1 text-xs">
        {entry.role === 'user' ? 'Developer' : 'Claude'}
      </span>
      <div className="border-border max-w-3xl rounded-lg border px-3 py-2">
        {entry.blocks.map((block, i) => (
          <Block key={i} block={block} toolResults={toolResults} />
        ))}
      </div>
    </div>
  );
}
