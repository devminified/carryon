// CO-406: a shared module that turns a session's (already laptop-side
// redacted — redaction.md) transcript into a condensed text for
// CO-407's not-yet-built summarize-session job to hand to the
// Anthropic API. Never calls out to anything itself; pure text
// processing plus one Storage-reading helper, both usable
// independently so the pure logic stays unit-testable without a real
// Supabase instance.
//
// Output shape (ticket's own wording): every developer prompt in
// full, Claude's final text reply for each turn, one line per tool
// call (tool, target, result status), and the deduplicated
// files-touched/commands-run lists. Bulky tool output (file contents,
// long logs, images) never appears — only ever a one-line summary of
// each tool call, never its raw content block.
//
// Never "expands" a redaction placeholder or otherwise reconstructs
// text around one: every string this module touches is copied
// verbatim from the parsed transcript (prompt text, reply text, a
// tool's own input fields) — there is no decoding/unescaping step
// that could turn a `[REDACTED:<rule-id>]` placeholder (redaction.md)
// into anything else, and no byte range here is computed from
// anything other than whole turns, so a cap or an incremental cut
// never slices into the middle of one.

import type { SupabaseClient } from "npm:@supabase/supabase-js@2.119.0";
import { decompressChunk } from "./zstd.ts";
import {
  type ContentBlock,
  isPureToolResultMessage,
  parseTranscript,
  type TranscriptEntry,
} from "./transcript-schema.ts";

// ~4 characters per token is the standard rough estimate for English
// text (no real tokenizer dependency — the ticket's own wording,
// "capped at *about* 60,000 tokens," tolerates an approximation; see
// this file's own test for how close it tracks a real count on the
// fixtures that matter here).
const CHARS_PER_TOKEN_ESTIMATE = 4;
export const DEFAULT_CAP_TOKENS = 60_000;

export function estimateTokens(text: string): number {
  return Math.ceil(text.length / CHARS_PER_TOKEN_ESTIMATE);
}

type ToolUseBlock = Extract<ContentBlock, { type: "tool_use" }>;
type ToolResultBlock = Extract<ContentBlock, { type: "tool_result" }>;

function str(v: unknown): string | null {
  return typeof v === "string" ? v : null;
}

function basename(path: string): string {
  return path.split("/").pop() ?? path;
}

// The tool names whose file_path input counts as a "file touched" —
// Read/Write/Edit/NotebookEdit all operate on one real file; Grep/Glob
// take a *pattern*, not a file, so they're deliberately excluded.
const FILE_TOOLS = new Set(["Read", "Write", "Edit", "NotebookEdit"]);

function toolTarget(toolUse: ToolUseBlock): string | null {
  switch (toolUse.name) {
    case "Read":
    case "Write":
    case "Edit":
    case "NotebookEdit": {
      const path = str(toolUse.input.file_path) ??
        str(toolUse.input.notebook_path);
      // the one-line tool-call summary shows just the basename (matching
      // the web viewer's own tool-summary.ts, CO-403) — files_touched
      // below still records the *full* path, since that's the exact
      // value a reviewer or a future "who touched this file" lookup
      // (CO-502) needs.
      return path ? basename(path) : null;
    }
    case "Bash":
      return str(toolUse.input.command);
    case "Grep":
    case "Glob":
      return str(toolUse.input.pattern);
    case "WebFetch":
    case "WebSearch":
      return str(toolUse.input.url) ?? str(toolUse.input.query);
    case "Task":
      return str(toolUse.input.description);
    default:
      return null;
  }
}

function toolCallLine(
  toolUse: ToolUseBlock,
  result: ToolResultBlock | null,
): string {
  const target = toolTarget(toolUse);
  const status = result ? (result.isError ? "error" : "ok") : "pending";
  return target
    ? `- ${toolUse.name} \`${target}\`: ${status}`
    : `- ${toolUse.name}: ${status}`;
}

// One turn = a real developer prompt (a user message that isn't pure
// tool_result output) plus everything up to the next one — Claude's
// reply, every tool call and its result, and any tool-result-only
// messages those tool calls produced. Mirrors the real schema's own
// shape (correlate.ts, CO-403): a tool_result always arrives in a
// *later* message than its tool_use, but still within the same turn.
interface Turn {
  entries: TranscriptEntry[];
}

function groupIntoTurns(entries: TranscriptEntry[]): Turn[] {
  const turns: Turn[] = [];
  let current: Turn | null = null;

  for (const entry of entries) {
    const startsNewTurn = entry.kind === "message" &&
      entry.role === "user" &&
      !isPureToolResultMessage(entry);
    if (startsNewTurn || current === null) {
      current = { entries: [] };
      turns.push(current);
    }
    current.entries.push(entry);
  }
  return turns;
}

function buildToolResultMap(turn: Turn): Map<string, ToolResultBlock> {
  const map = new Map<string, ToolResultBlock>();
  for (const entry of turn.entries) {
    if (entry.kind !== "message") continue;
    for (const block of entry.blocks) {
      if (block.type === "tool_result") map.set(block.toolUseId, block);
    }
  }
  return map;
}

// No "Turn N" header: a turn's rendered number would depend on its
// absolute position in the *whole* session, which an incremental call
// condensing only one range of chunks has no way to know — baking a
// position-dependent label in here would silently break the "earlier
// range's turns + later range's turns == condensing everything at
// once" equality this module exists to guarantee. The **Developer:**/
// **Claude:** markers already delimit each turn visually.
function renderTurn(turn: Turn): string {
  const resultsByToolUseId = buildToolResultMap(turn);

  let userText: string | null = null;
  let lastAssistantText: string | null = null;
  // Claude Code's own "/compact" checkpoints (kind: "summary") can
  // land anywhere, not only at a session's true end — tracked the
  // same way as lastAssistantText (last one seen wins) so its
  // position in the rendered output is fixed regardless of exactly
  // where it fell in the raw entries, not wherever a naive "push
  // immediately on encounter" loop would leave it relative to the
  // other categories below (a real bug this fixed — see this
  // function's git history).
  let lastSummary: string | null = null;
  const toolLines: string[] = [];

  for (const entry of turn.entries) {
    if (entry.kind === "summary") {
      lastSummary = entry.summary;
      continue;
    }
    if (entry.kind === "unknown") continue;
    if (entry.kind !== "message") continue;

    for (const block of entry.blocks) {
      if (block.type === "text") {
        if (entry.role === "user") userText = (userText ?? "") + block.text;
        else lastAssistantText = (lastAssistantText ?? "") + block.text;
      } else if (block.type === "tool_use") {
        toolLines.push(
          toolCallLine(block, resultsByToolUseId.get(block.id) ?? null),
        );
      }
    }
  }

  const lines: string[] = [];
  if (userText !== null) lines.push(`**Developer:** ${userText}`);
  if (toolLines.length > 0) lines.push(...toolLines);
  if (lastAssistantText !== null) {
    lines.push(`**Claude:** ${lastAssistantText}`);
  }
  if (lastSummary !== null) lines.push(`_Session summary: ${lastSummary}_`);

  return lines.join("\n");
}

export interface CondenseResult {
  /** One rendered block per turn, in order — never capped (see
   * applyTokenCap below for that). Incremental mode (condensing a
   * later range of chunks on its own) produces exactly the turns found
   * in that range; concatenating an earlier range's turns with a later
   * range's turns equals condensing the whole thing in one call,
   * *provided* the cut between ranges lands on a turn boundary — the
   * contract CO-407's own seq bookkeeping must uphold (this module has
   * no way to know where a caller's chunk range was cut). */
  turns: string[];
  filesTouched: string[];
  commandsRun: string[];
}

/** Condenses already-decompressed, seq-ordered JSONL text (one or more
 * chunks' worth, concatenated) into turn blocks plus the deduplicated
 * files-touched/commands-run lists — no cap applied. Call
 * applyTokenCap on the result's `turns` once every range a session has
 * is assembled, not per range, since the cap is a property of the
 * whole session's output, not of any one incremental slice. */
export function condenseTranscriptText(text: string): CondenseResult {
  const entries = parseTranscript(text);
  const turns = groupIntoTurns(entries);

  const filesTouched = new Set<string>();
  const commandsRun = new Set<string>();
  for (const entry of entries) {
    if (entry.kind !== "message") continue;
    for (const block of entry.blocks) {
      if (block.type !== "tool_use") continue;
      if (FILE_TOOLS.has(block.name)) {
        const path = str(block.input.file_path) ??
          str(block.input.notebook_path);
        if (path) filesTouched.add(path);
      } else if (block.name === "Bash") {
        const command = str(block.input.command);
        if (command) commandsRun.add(command);
      }
    }
  }

  return {
    turns: turns.map(renderTurn),
    filesTouched: [...filesTouched].sort(),
    commandsRun: [...commandsRun].sort(),
  };
}

const OMISSION_MARKER = (n: number) =>
  `\n\n_[... ${n} earlier turn${
    n === 1 ? "" : "s"
  } omitted to stay under the token cap ...]_\n\n`;

/** Drops the oldest *middle* turns first (the ticket's own wording) —
 * the first and last turns always survive, since those are what most
 * reliably say what a session started out to do and where it ended
 * up. Never truncates *within* a turn — a single turn's own text can
 * still leave the final output a little over capTokens; see this
 * file's own test for why that's an accepted, documented edge case
 * rather than a half-built within-turn truncation nobody asked for.
 *
 * A single linear pass over the middle turns, walked from the most
 * recent backward, keeping whatever fits in the remaining budget —
 * not repeated splice-and-recheck, which would be O(turns²) and the
 * first thing to fall over on the ticket's own "200MB session" case
 * (tens of thousands of turns, almost all of them dropped). */
export function applyTokenCap(
  turns: string[],
  capTokens = DEFAULT_CAP_TOKENS,
): string {
  if (turns.length === 0) return "";
  if (turns.length <= 2) return turns.join("\n\n");

  const first = turns[0];
  const last = turns[turns.length - 1];
  const middle = turns.slice(1, -1); // chronological order, oldest first

  // Each kept turn costs its own text plus the "\n\n" join that will
  // sit next to it in the final output — with thousands of turns in
  // play (this file's own "200MB" test), that per-turn join overhead
  // is not negligible and has to be budgeted for, not just the turns'
  // own text.
  const JOIN_TOKENS = estimateTokens("\n\n");
  let budget = capTokens - estimateTokens(first) - estimateTokens(last) -
    JOIN_TOKENS;
  let keepFromIndex = middle.length;
  for (let i = middle.length - 1; i >= 0; i--) {
    const cost = estimateTokens(middle[i]) + JOIN_TOKENS;
    if (cost > budget) break;
    budget -= cost;
    keepFromIndex = i;
  }
  const keptMiddle = middle.slice(keepFromIndex);
  const droppedCount = middle.length - keptMiddle.length;

  if (droppedCount === 0) return [first, ...keptMiddle, last].join("\n\n");
  return [first, OMISSION_MARKER(droppedCount).trim(), ...keptMiddle, last]
    .join("\n\n");
}

/** One-shot convenience: condense a whole session's chunk text (not
 * incremental) and cap it in the same call. */
export function condenseSession(
  text: string,
  capTokens = DEFAULT_CAP_TOKENS,
): { text: string; filesTouched: string[]; commandsRun: string[] } {
  const result = condenseTranscriptText(text);
  return {
    text: applyTokenCap(result.turns, capTokens),
    filesTouched: result.filesTouched,
    commandsRun: result.commandsRun,
  };
}

interface ChunkRow {
  seq: number;
  object_key: string;
}

/** Reads a session's main-stream chunks (not subagent streams — those
 * are separate, parallel conversations that don't fit this module's
 * one-sequence-of-turns model) from Storage, decompressed, in seq
 * order. afterSeq supports incremental mode (the ticket's own
 * wording): pass the seq of the last chunk already condensed to read
 * only what's new. Returns the highest seq actually read, so a caller
 * can checkpoint it for next time; null bucket/chunk gaps are each
 * other's problem to solve (register-chunk's own idempotency, not
 * this module's) — this just reads what session_chunks says exists. */
export async function readSessionMainChunks(
  admin: SupabaseClient,
  bucket: string,
  sessionId: string,
  opts: { afterSeq?: number } = {},
): Promise<{ texts: string[]; lastSeq: number | null }> {
  let query = admin
    .from("session_chunks")
    .select("seq, object_key")
    .eq("session_id", sessionId)
    .eq("stream_id", "main")
    .order("seq", { ascending: true });
  if (opts.afterSeq !== undefined) {
    query = query.gt("seq", opts.afterSeq);
  }

  const { data, error } = await query;
  if (error) throw error;
  const rows = (data ?? []) as ChunkRow[];
  if (rows.length === 0) return { texts: [], lastSeq: null };

  const texts: string[] = [];
  for (const row of rows) {
    const { data: blob, error: downloadErr } = await admin.storage
      .from(bucket)
      .download(row.object_key);
    if (downloadErr || !blob) {
      throw new Error(
        `readSessionMainChunks: download failed for ${bucket}/${row.object_key}: ${
          downloadErr?.message ?? "no data"
        }`,
      );
    }
    const compressed = new Uint8Array(await blob.arrayBuffer());
    const plaintext = await decompressChunk(compressed);
    texts.push(new TextDecoder().decode(plaintext));
  }

  return { texts, lastSeq: rows[rows.length - 1].seq };
}
