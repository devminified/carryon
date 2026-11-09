// Parses Claude Code's real transcript JSONL schema — the same shape
// internal/redact/walk.go already depends on (type:"tool_use"/"tool_result",
// "id"/"tool_use_id", "name", "input.file_path", "content" as a string or
// an array of blocks): each line's `message.content` is either a plain
// string or an array of content blocks (`text`, `tool_use`, `tool_result`).
//
// Deliberately defensive (CO-403's own testing point): anything that
// doesn't match this shape — a future Claude Code format, a hand-edited
// fixture, a bug in this parser's own assumptions about fields walk.go
// never had to care about (timestamp, uuid, gitBranch, …) — becomes an
// `unknown` entry carrying the raw line, rendered as JSON rather than
// crashing the page.

export type ContentBlock =
  | { type: 'text'; text: string }
  | { type: 'tool_use'; id: string; name: string; input: Record<string, unknown> }
  | { type: 'tool_result'; toolUseId: string; content: string | ContentBlock[]; isError: boolean }
  | { type: 'unknown'; raw: unknown };

export type TranscriptEntry =
  | { kind: 'message'; role: 'user' | 'assistant'; uuid: string | null; blocks: ContentBlock[] }
  | { kind: 'summary'; uuid: string | null; summary: string }
  | { kind: 'unknown'; raw: unknown };

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === 'object' && v !== null && !Array.isArray(v);
}

function parseContentBlock(raw: unknown): ContentBlock {
  if (!isRecord(raw) || typeof raw.type !== 'string') {
    return { type: 'unknown', raw };
  }
  switch (raw.type) {
    case 'text':
      return typeof raw.text === 'string'
        ? { type: 'text', text: raw.text }
        : { type: 'unknown', raw };
    case 'tool_use':
      return typeof raw.id === 'string' && typeof raw.name === 'string' && isRecord(raw.input)
        ? { type: 'tool_use', id: raw.id, name: raw.name, input: raw.input }
        : { type: 'unknown', raw };
    case 'tool_result': {
      if (typeof raw.tool_use_id !== 'string') return { type: 'unknown', raw };
      const content =
        typeof raw.content === 'string'
          ? raw.content
          : Array.isArray(raw.content)
            ? raw.content.map(parseContentBlock)
            : '';
      return {
        type: 'tool_result',
        toolUseId: raw.tool_use_id,
        content,
        isError: raw.is_error === true,
      };
    }
    default:
      return { type: 'unknown', raw };
  }
}

function parseContent(raw: unknown): ContentBlock[] {
  if (typeof raw === 'string') return [{ type: 'text', text: raw }];
  if (Array.isArray(raw)) return raw.map(parseContentBlock);
  return [{ type: 'unknown', raw }];
}

export function parseTranscriptLine(line: string): TranscriptEntry {
  let raw: unknown;
  try {
    raw = JSON.parse(line);
  } catch {
    return { kind: 'unknown', raw: line };
  }
  if (!isRecord(raw) || typeof raw.type !== 'string') {
    return { kind: 'unknown', raw };
  }

  if (raw.type === 'summary' && typeof raw.summary === 'string') {
    return {
      kind: 'summary',
      uuid: typeof raw.uuid === 'string' ? raw.uuid : null,
      summary: raw.summary,
    };
  }

  if (
    (raw.type === 'user' || raw.type === 'assistant') &&
    isRecord(raw.message) &&
    (raw.message.role === 'user' || raw.message.role === 'assistant')
  ) {
    return {
      kind: 'message',
      role: raw.message.role,
      uuid: typeof raw.uuid === 'string' ? raw.uuid : null,
      blocks: parseContent(raw.message.content),
    };
  }

  return { kind: 'unknown', raw };
}

// A stream's bytes, once decompressed and concatenated in seq order
// (internal/pull.pull.go's own rebuild order), are plain newline-
// delimited JSON — split on '\n', skip empty trailing lines (the file
// always ends in one, same as any JSONL writer).
export function parseTranscript(text: string): TranscriptEntry[] {
  return text
    .split('\n')
    .filter((line) => line.length > 0)
    .map(parseTranscriptLine);
}
