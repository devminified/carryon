// CO-406: a hand-kept TS port of the real Claude Code transcript JSONL
// schema that `web/src/modules/sessions/viewer/lib/parse-transcript.ts`
// (CO-403) already parses, and that `internal/redact/walk.go` depends
// on (type:"tool_use"/"tool_result", "id"/"tool_use_id", "name",
// "input.file_path", "content" as a string or an array of blocks).
// Not imported across the web/Deno boundary — same reasoning as
// redact.ts's own header comment: these are two different runtimes in
// two different trust zones (architecture.md), kept in sync by hand.
// If this file and parse-transcript.ts ever drift on what the schema
// allows, that's the signal to update whichever one fell behind.
//
// Deliberately defensive, same as parse-transcript.ts: anything that
// doesn't match this shape becomes an `unknown` entry carrying the raw
// line rather than throwing — a condensed transcript with an occasional
// "(unrecognized line)" is far better than one real line crashing the
// whole summary job.

export type ContentBlock =
  | { type: "text"; text: string }
  | {
    type: "tool_use";
    id: string;
    name: string;
    input: Record<string, unknown>;
  }
  | {
    type: "tool_result";
    toolUseId: string;
    content: string | ContentBlock[];
    isError: boolean;
  }
  | { type: "unknown"; raw: unknown };

export type TranscriptEntry =
  | { kind: "message"; role: "user" | "assistant"; blocks: ContentBlock[] }
  | { kind: "summary"; summary: string }
  | { kind: "unknown"; raw: unknown };

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function parseContentBlock(raw: unknown): ContentBlock {
  if (!isRecord(raw) || typeof raw.type !== "string") {
    return { type: "unknown", raw };
  }
  switch (raw.type) {
    case "text":
      return typeof raw.text === "string"
        ? { type: "text", text: raw.text }
        : { type: "unknown", raw };
    case "tool_use":
      return typeof raw.id === "string" &&
          typeof raw.name === "string" &&
          isRecord(raw.input)
        ? { type: "tool_use", id: raw.id, name: raw.name, input: raw.input }
        : { type: "unknown", raw };
    case "tool_result": {
      if (typeof raw.tool_use_id !== "string") return { type: "unknown", raw };
      const content = typeof raw.content === "string"
        ? raw.content
        : Array.isArray(raw.content)
        ? raw.content.map(parseContentBlock)
        : "";
      return {
        type: "tool_result",
        toolUseId: raw.tool_use_id,
        content,
        isError: raw.is_error === true,
      };
    }
    default:
      return { type: "unknown", raw };
  }
}

function parseContent(raw: unknown): ContentBlock[] {
  if (typeof raw === "string") return [{ type: "text", text: raw }];
  if (Array.isArray(raw)) return raw.map(parseContentBlock);
  return [{ type: "unknown", raw }];
}

export function parseTranscriptLine(line: string): TranscriptEntry {
  let raw: unknown;
  try {
    raw = JSON.parse(line);
  } catch {
    return { kind: "unknown", raw: line };
  }
  if (!isRecord(raw) || typeof raw.type !== "string") {
    return { kind: "unknown", raw };
  }

  if (raw.type === "summary" && typeof raw.summary === "string") {
    return { kind: "summary", summary: raw.summary };
  }

  if (
    (raw.type === "user" || raw.type === "assistant") &&
    isRecord(raw.message) &&
    (raw.message.role === "user" || raw.message.role === "assistant")
  ) {
    return {
      kind: "message",
      role: raw.message.role,
      blocks: parseContent(raw.message.content),
    };
  }

  return { kind: "unknown", raw };
}

/** text is one or more chunks' decompressed JSONL, already concatenated
 * in seq order — plain newline-delimited JSON, same as
 * parse-transcript.ts's own parseTranscript. */
export function parseTranscript(text: string): TranscriptEntry[] {
  return text
    .split("\n")
    .filter((line) => line.length > 0)
    .map(parseTranscriptLine);
}

/** A user message whose blocks are *only* tool_result blocks carries no
 * prompt of its own — Claude Code's schema returns tool output this
 * way (correlate.ts's own isPureToolResultMessage, CO-403). Condensing
 * groups it into the turn still in progress rather than treating it as
 * a new developer prompt. */
export function isPureToolResultMessage(entry: TranscriptEntry): boolean {
  return (
    entry.kind === "message" &&
    entry.role === "user" &&
    entry.blocks.length > 0 &&
    entry.blocks.every((b) => b.type === "tool_result")
  );
}
