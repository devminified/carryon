import type { ContentBlock, TranscriptEntry } from './parse-transcript';

type ToolResultBlock = Extract<ContentBlock, { type: 'tool_result' }>;

// A tool_use lives in one assistant message; its tool_result arrives in
// a *later* user message (real Claude Code schema — see
// parse-transcript.ts's own doc comment). Building one map up front
// lets each tool_use render its paired result immediately once both
// have streamed in, regardless of which chunk either landed in.
export function buildToolResultMap(entries: TranscriptEntry[]): Map<string, ToolResultBlock> {
  const map = new Map<string, ToolResultBlock>();
  for (const entry of entries) {
    if (entry.kind !== 'message') continue;
    for (const block of entry.blocks) {
      if (block.type === 'tool_result') {
        map.set(block.toolUseId, block);
      }
    }
  }
  return map;
}

// A user message whose blocks are *only* tool_result blocks is the
// real schema's way of returning tool output — it has nothing of its
// own to show as a chat bubble; its content is already reachable via
// the paired tool_use's expandable details (ui/tool-call.tsx). Filters
// it out of the main visible flow; summary/unknown entries and any
// message that mixes in real text always stay visible.
export function isPureToolResultMessage(entry: TranscriptEntry): boolean {
  return (
    entry.kind === 'message' &&
    entry.blocks.length > 0 &&
    entry.blocks.every((b) => b.type === 'tool_result')
  );
}

export function visibleEntries(entries: TranscriptEntry[]): TranscriptEntry[] {
  return entries.filter((e) => !isPureToolResultMessage(e));
}
