'use client';

import { useState } from 'react';
import type { ContentBlock } from '../lib/parse-transcript';
import { summarizeToolCall } from '../lib/tool-summary';
import { Button } from '@/shared/ui/button';
import { DiffView } from './diff-view';
import { HighlightedPre } from './highlighted-text';

type ToolUseBlock = Extract<ContentBlock, { type: 'tool_use' }>;
type ToolResultBlock = Extract<ContentBlock, { type: 'tool_result' }>;

function resultText(content: ToolResultBlock['content']): string {
  if (typeof content === 'string') return content;
  return content.map((b) => (b.type === 'text' ? b.text : JSON.stringify(b))).join('\n');
}

export function ToolCall({
  toolUse,
  result,
}: {
  toolUse: ToolUseBlock;
  result: ToolResultBlock | null;
}) {
  const [open, setOpen] = useState(false);
  const summary = summarizeToolCall(toolUse, result);
  const isEdit =
    toolUse.name === 'Edit' &&
    typeof toolUse.input.old_string === 'string' &&
    typeof toolUse.input.new_string === 'string';

  return (
    <div className="border-border my-1 rounded border">
      <Button
        type="button"
        variant="ghost"
        onClick={() => setOpen((v) => !v)}
        className="h-auto w-full justify-start gap-2 rounded-none px-2 py-1 text-sm font-normal"
      >
        <span aria-hidden="true">{open ? '▾' : '▸'}</span>
        <span className="font-mono">{summary}</span>
        {result?.isError && (
          <span className="rounded bg-red-100 px-1.5 text-xs text-red-800 dark:bg-red-900/40 dark:text-red-100">
            error
          </span>
        )}
        {!result && <span className="text-muted-foreground text-xs">running…</span>}
      </Button>
      {open && (
        <div className="border-border border-t p-2">
          {isEdit ? (
            <DiffView
              oldText={toolUse.input.old_string as string}
              newText={toolUse.input.new_string as string}
            />
          ) : (
            <>
              <p className="text-muted-foreground mb-1 text-xs font-semibold">Input</p>
              <HighlightedPre text={JSON.stringify(toolUse.input, null, 2)} />
            </>
          )}
          {result && (
            <>
              <p className="text-muted-foreground mt-2 mb-1 text-xs font-semibold">Output</p>
              <HighlightedPre text={resultText(result.content)} />
            </>
          )}
        </div>
      )}
    </div>
  );
}
