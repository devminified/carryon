import { diffLines } from 'diff';
import type { ContentBlock } from './parse-transcript';

type ToolUseBlock = Extract<ContentBlock, { type: 'tool_use' }>;
type ToolResultBlock = Extract<ContentBlock, { type: 'tool_result' }>;

function basename(path: string): string {
  return path.split('/').pop() ?? path;
}

function str(v: unknown): string | null {
  return typeof v === 'string' ? v : null;
}

export function diffCounts(oldText: string, newText: string): { added: number; removed: number } {
  const parts = diffLines(oldText, newText);
  let added = 0;
  let removed = 0;
  for (const part of parts) {
    const lines = part.value.split('\n').filter((l) => l.length > 0).length;
    if (part.added) added += lines;
    if (part.removed) removed += lines;
  }
  return { added, removed };
}

// One-line summaries matching the ticket's own examples literally
// ("Edited `webhook.ts` (+12 −03)", "Ran `npm test`, exit 1") — unknown
// tool names fall back to a generic "Ran `<name>`" rather than guessing
// at a shape that isn't there (same defensive spirit as parse-transcript).
export function summarizeToolCall(toolUse: ToolUseBlock, result: ToolResultBlock | null): string {
  const input = toolUse.input;
  switch (toolUse.name) {
    case 'Edit': {
      const filePath = str(input.file_path);
      const oldString = str(input.old_string) ?? '';
      const newString = str(input.new_string) ?? '';
      if (!filePath) break;
      const { added, removed } = diffCounts(oldString, newString);
      return `Edited \`${basename(filePath)}\` (+${added} −${removed})`;
    }
    case 'Write': {
      const filePath = str(input.file_path);
      if (filePath) return `Wrote \`${basename(filePath)}\``;
      break;
    }
    case 'Read': {
      const filePath = str(input.file_path);
      if (filePath) return `Read \`${basename(filePath)}\``;
      break;
    }
    case 'Bash': {
      const command = str(input.command);
      if (command) {
        const suffix = result?.isError ? ', failed' : '';
        return `Ran \`${command}\`${suffix}`;
      }
      break;
    }
    case 'Grep': {
      const pattern = str(input.pattern);
      if (pattern) return `Searched for \`${pattern}\``;
      break;
    }
    case 'Glob': {
      const pattern = str(input.pattern);
      if (pattern) return `Listed files matching \`${pattern}\``;
      break;
    }
    case 'WebFetch': {
      const url = str(input.url);
      if (url) return `Fetched \`${url}\``;
      break;
    }
    case 'Task': {
      const description = str(input.description);
      if (description) return `Started subagent: ${description}`;
      break;
    }
  }
  return `Ran \`${toolUse.name}\``;
}
