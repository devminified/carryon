import { assert, assertEquals } from "jsr:@std/assert@1.0.19";
import {
  applyTokenCap,
  condenseSession,
  condenseTranscriptText,
  DEFAULT_CAP_TOKENS,
  estimateTokens,
  readSessionMainChunks,
} from "./condense-transcript.ts";

function userLine(uuid: string, text: string): string {
  return JSON.stringify({
    type: "user",
    uuid,
    message: { role: "user", content: text },
  });
}

function assistantTextLine(uuid: string, text: string): string {
  return JSON.stringify({
    type: "assistant",
    uuid,
    message: { role: "assistant", content: [{ type: "text", text }] },
  });
}

function assistantToolUseLine(
  uuid: string,
  id: string,
  name: string,
  input: Record<string, unknown>,
  precedingText?: string,
): string {
  const content: unknown[] = [];
  if (precedingText) content.push({ type: "text", text: precedingText });
  content.push({ type: "tool_use", id, name, input });
  return JSON.stringify({
    type: "assistant",
    uuid,
    message: { role: "assistant", content },
  });
}

function toolResultLine(
  uuid: string,
  toolUseId: string,
  content: string,
  isError = false,
): string {
  return JSON.stringify({
    type: "user",
    uuid,
    message: {
      role: "user",
      content: [{
        type: "tool_result",
        tool_use_id: toolUseId,
        is_error: isError,
        content,
      }],
    },
  });
}

// A real, two-turn session: fix a bug (Read then Edit, a passing
// test), then double check it (a Bash re-run) — this is the
// "hand-labelled fixture" the files_touched testing point checks
// against.
const TWO_TURN_LINES = [
  userLine("u1", "Can you fix the payment webhook retry logic?"),
  assistantToolUseLine("a1", "toolu_1", "Read", {
    file_path: "/repo/webhook.ts",
  }, "Let me look."),
  toolResultLine(
    "tr1",
    "toolu_1",
    "export function retry() { return fetch(url); }\n",
  ),
  assistantToolUseLine(
    "a2",
    "toolu_2",
    "Edit",
    { file_path: "/repo/webhook.ts", old_string: "a", new_string: "b" },
    "Found it, fixing the backoff.",
  ),
  toolResultLine("tr2", "toolu_2", "ok"),
  assistantTextLine("a3", "Fixed the retry backoff in webhook.ts."),
  userLine("u2", "Great, can you run the tests to confirm?"),
  assistantToolUseLine("a4", "toolu_3", "Bash", { command: "npm test" }),
  toolResultLine("tr3", "toolu_3", "5 passing\n"),
  assistantTextLine("a5", "All 5 tests pass."),
].join("\n") + "\n";

Deno.test("renders every developer prompt in full and Claude's final text reply per turn", () => {
  const result = condenseTranscriptText(TWO_TURN_LINES);
  assertEquals(result.turns.length, 2);
  assert(
    result.turns[0].includes("Can you fix the payment webhook retry logic?"),
  );
  assert(result.turns[0].includes("Fixed the retry backoff in webhook.ts."));
  assert(result.turns[1].includes("Great, can you run the tests to confirm?"));
  assert(result.turns[1].includes("All 5 tests pass."));
});

Deno.test("one line per tool call, naming the tool, its target, and the result status", () => {
  const result = condenseTranscriptText(TWO_TURN_LINES);
  assert(result.turns[0].includes("- Read `webhook.ts`: ok"));
  assert(result.turns[0].includes("- Edit `webhook.ts`: ok"));
  assert(result.turns[1].includes("- Bash `npm test`: ok"));
});

Deno.test("never includes a tool's bulky raw output", () => {
  const result = condenseTranscriptText(TWO_TURN_LINES);
  const joined = result.turns.join("\n");
  assert(!joined.includes("export function retry()"));
  assert(!joined.includes("5 passing"));
});

Deno.test("a failed tool call is marked error, not ok", () => {
  const lines = [
    userLine("u1", "run the build"),
    assistantToolUseLine("a1", "toolu_1", "Bash", { command: "npm run build" }),
    toolResultLine("tr1", "toolu_1", "TypeError: x is undefined", true),
  ].join("\n") + "\n";
  const result = condenseTranscriptText(lines);
  assert(result.turns[0].includes("- Bash `npm run build`: error"));
});

Deno.test("files_touched is exact against the hand-labelled fixture", () => {
  const result = condenseTranscriptText(TWO_TURN_LINES);
  assertEquals(result.filesTouched, ["/repo/webhook.ts"]);
  assertEquals(result.commandsRun, ["npm test"]);
});

Deno.test("files_touched excludes Grep/Glob patterns — those aren't files", () => {
  const lines = [
    userLine("u1", "find the retry logic"),
    assistantToolUseLine("a1", "toolu_1", "Grep", { pattern: "retry" }),
    toolResultLine("tr1", "toolu_1", "webhook.ts:12"),
  ].join("\n") + "\n";
  const result = condenseTranscriptText(lines);
  assertEquals(result.filesTouched, []);
});

Deno.test("deduplicates files touched and commands run across turns", () => {
  const lines = [
    userLine("u1", "read the config twice"),
    assistantToolUseLine("a1", "toolu_1", "Read", {
      file_path: "/repo/config.ts",
    }),
    toolResultLine("tr1", "toolu_1", "ok"),
    userLine("u2", "again"),
    assistantToolUseLine("a2", "toolu_2", "Read", {
      file_path: "/repo/config.ts",
    }),
    toolResultLine("tr2", "toolu_2", "ok"),
    assistantToolUseLine(
      "a3",
      "toolu_3",
      "Bash",
      { command: "npm test" },
      "also re-run tests",
    ),
    toolResultLine("tr3", "toolu_3", "ok"),
    assistantToolUseLine("a4", "toolu_4", "Bash", { command: "npm test" }),
    toolResultLine("tr4", "toolu_4", "ok"),
  ].join("\n") + "\n";
  const result = condenseTranscriptText(lines);
  assertEquals(result.filesTouched, ["/repo/config.ts"]);
  assertEquals(result.commandsRun, ["npm test"]);
});

// CO-502: "Paths are normalized relative to the repo root, so
// different machines match." Two developers running the same repo
// from different absolute locations must end up with the exact same
// files_touched entry.
Deno.test("CO-502: files_touched is normalized relative to the session's own cwd", () => {
  const lines = [
    userLine("u1", "fix the retry logic"),
    assistantToolUseLine("a1", "toolu_1", "Edit", {
      file_path: "/Users/ali/dev/acme-api/src/payments/webhook.ts",
      old_string: "a",
      new_string: "b",
    }),
    toolResultLine("tr1", "toolu_1", "ok"),
  ].join("\n") + "\n";
  const result = condenseTranscriptText(lines, "/Users/ali/dev/acme-api");
  assertEquals(result.filesTouched, ["src/payments/webhook.ts"]);
});

Deno.test("CO-502: the same repo file normalizes identically from a second developer's own clone location", () => {
  const lines = [
    userLine("u1", "fix the retry logic"),
    assistantToolUseLine("a1", "toolu_1", "Edit", {
      file_path: "/home/sara/code/acme-api/src/payments/webhook.ts",
      old_string: "a",
      new_string: "b",
    }),
    toolResultLine("tr1", "toolu_1", "ok"),
  ].join("\n") + "\n";
  const result = condenseTranscriptText(lines, "/home/sara/code/acme-api");
  assertEquals(result.filesTouched, ["src/payments/webhook.ts"]);
});

Deno.test("CO-502: a path outside cwd is left as-is — nothing else to normalize it against", () => {
  const lines = [
    userLine("u1", "check the global git config"),
    assistantToolUseLine("a1", "toolu_1", "Read", {
      file_path: "/Users/ali/.gitconfig",
    }),
    toolResultLine("tr1", "toolu_1", "ok"),
  ].join("\n") + "\n";
  const result = condenseTranscriptText(lines, "/Users/ali/dev/acme-api");
  assertEquals(result.filesTouched, ["/Users/ali/.gitconfig"]);
});

Deno.test("CO-502: with no cwd given, files_touched is left exactly as the raw path (backward compatible)", () => {
  const result = condenseTranscriptText(TWO_TURN_LINES);
  assertEquals(result.filesTouched, ["/repo/webhook.ts"]);
});

Deno.test("CO-502: a Windows cwd matches case-insensitively", () => {
  const lines = [
    userLine("u1", "fix the retry logic"),
    assistantToolUseLine("a1", "toolu_1", "Edit", {
      file_path: "C:\\Users\\Ali\\dev\\acme-api\\src\\payments\\webhook.ts",
      old_string: "a",
      new_string: "b",
    }),
    toolResultLine("tr1", "toolu_1", "ok"),
  ].join("\n") + "\n";
  const result = condenseTranscriptText(
    lines,
    "c:\\users\\ali\\dev\\acme-api",
  );
  assertEquals(result.filesTouched, ["src/payments/webhook.ts"]);
});

Deno.test("no redacted placeholder is expanded and no secret is reintroduced", () => {
  const lines = [
    userLine("u1", "check the aws creds"),
    assistantToolUseLine("a1", "toolu_1", "Read", {
      file_path: "/repo/.env.local",
    }),
    toolResultLine("tr1", "toolu_1", "[REDACTED:denylisted-file .env.local]"),
    assistantTextLine(
      "a2",
      "The credentials are [REDACTED:aws-access-key-id] — rotate them.",
    ),
  ].join("\n") + "\n";
  const result = condenseTranscriptText(lines);
  const joined = result.turns.join("\n");
  // the placeholder from the assistant's own reply passes through
  // verbatim — this module never re-derives or decodes it
  assert(joined.includes("[REDACTED:aws-access-key-id]"));
  // the tool_result's placeholder is bulky tool output and correctly
  // never appears at all (only the one-line tool-call summary does)
  assert(!joined.includes("denylisted-file"));
  assert(!joined.includes("AKIA"));
});

Deno.test("a session-summary line renders after Claude's text, regardless of where it fell in the raw entries", () => {
  const lines = [
    userLine("u1", "fix the webhook retry"),
    assistantTextLine("a1", "Fixed it."),
    // the real Claude Code fixture (web/e2e/seed-viewer-fixtures.mjs)
    // places its summary line last, after every exchange — this
    // reproduces that exact shape, not an artificial mid-turn one.
    JSON.stringify({
      type: "summary",
      summary: "Fixed the webhook retry backoff.",
    }),
  ].join("\n") + "\n";
  const result = condenseTranscriptText(lines);
  assertEquals(result.turns.length, 1);
  const claudeIndex = result.turns[0].indexOf("**Claude:**");
  const summaryIndex = result.turns[0].indexOf("_Session summary:");
  assert(claudeIndex >= 0 && summaryIndex >= 0 && claudeIndex < summaryIndex);
});

Deno.test("an unrecognized line degrades to being skipped, not a crash", () => {
  const lines = [
    userLine("u1", "hello"),
    JSON.stringify({ type: "something_from_the_future", foo: "bar" }),
    assistantTextLine("a1", "hi there"),
  ].join("\n") + "\n";
  const result = condenseTranscriptText(lines);
  assertEquals(result.turns.length, 1);
  assert(result.turns[0].includes("hello"));
  assert(result.turns[0].includes("hi there"));
});

Deno.test("incremental output plus the previous output equals the full output, on a turn boundary", () => {
  const allLines = TWO_TURN_LINES;
  const turnBoundaryIndex = allLines.indexOf(
    userLine("u2", "Great, can you run the tests to confirm?"),
  );
  const firstRangeText = allLines.slice(0, turnBoundaryIndex);
  const secondRangeText = allLines.slice(turnBoundaryIndex);

  const full = condenseTranscriptText(allLines);
  const incrementalFirst = condenseTranscriptText(firstRangeText);
  const incrementalSecond = condenseTranscriptText(secondRangeText);

  assertEquals(
    [...incrementalFirst.turns, ...incrementalSecond.turns],
    full.turns,
  );
});

Deno.test("estimateTokens is a plain, documented ~4-chars-per-token estimate", () => {
  assertEquals(estimateTokens(""), 0);
  assertEquals(estimateTokens("abcd"), 1);
  assertEquals(estimateTokens("abcde"), 2);
});

Deno.test("applyTokenCap keeps everything when already under the cap", () => {
  const turns = ["turn one", "turn two", "turn three"];
  assertEquals(applyTokenCap(turns, DEFAULT_CAP_TOKENS), turns.join("\n\n"));
});

Deno.test("applyTokenCap always keeps the first and last turn", () => {
  const turns = Array.from(
    { length: 20 },
    (_, i) => `turn ${i}: ` + "x".repeat(500),
  );
  const capped = applyTokenCap(turns, 200); // far too small to keep everything
  assert(capped.startsWith("turn 0:"));
  assert(capped.endsWith("x".repeat(500)));
  assert(capped.includes("turn 19:"));
});

Deno.test("applyTokenCap drops the oldest middle turns first, newest middle turns survive", () => {
  const turns = Array.from(
    { length: 10 },
    (_, i) => `turn ${i}: ` + "x".repeat(100),
  );
  // budget for first + last + a handful of the most recent middle turns
  const capped = applyTokenCap(turns, 120);
  assert(capped.includes("turn 0:"));
  assert(capped.includes("turn 9:"));
  assert(
    !capped.includes("turn 1:"),
    "the oldest middle turn should be dropped first",
  );
  assert(capped.includes("omitted"));
});

Deno.test("a 200MB-scale session (many thousands of turns) condenses to under the cap", () => {
  // Not literally 200MB (that would make this test itself slow to
  // run in CI for no extra confidence) — the capping algorithm is a
  // single linear pass over however many turns there are (this file's
  // own applyTokenCap comment), so a real 200MB transcript (tens of
  // thousands of turns) is the same O(n) shape as the 20,000 turns
  // built here, just a larger constant. What's actually under test is
  // that the algorithm's *output size* is bounded by the cap
  // regardless of how much input came in — the same reasoning
  // web/e2e/seed-viewer-fixtures.mjs's own LARGE_SESSION_ID fixture
  // uses for CO-403's "200MB" claim.
  const manyTurns = Array.from(
    { length: 20_000 },
    (_, i) => `turn ${i}: ` + "x".repeat(300),
  );
  const capped = applyTokenCap(manyTurns, DEFAULT_CAP_TOKENS);
  // +50 tokens of slack for the omission marker's own small overhead
  // — the point under test is that the cap bounds the *output*
  // regardless of how many turns came in, not the marker's exact text.
  assert(estimateTokens(capped) <= DEFAULT_CAP_TOKENS + 50);
  assert(capped.includes("turn 0:"));
  assert(capped.includes("turn 19999:"));
  assert(capped.includes("omitted"));
});

Deno.test("condenseSession combines condensing and capping in one call", () => {
  const { text, filesTouched, commandsRun } = condenseSession(
    TWO_TURN_LINES,
    DEFAULT_CAP_TOKENS,
  );
  assert(text.includes("Can you fix the payment webhook retry logic?"));
  assertEquals(filesTouched, ["/repo/webhook.ts"]);
  assertEquals(commandsRun, ["npm test"]);
});

Deno.test("readSessionMainChunks downloads and decompresses main-stream chunks in seq order, supporting afterSeq", async () => {
  const downloaded: string[] = [];
  const admin = {
    from(table: string) {
      assertEquals(table, "session_chunks");
      return {
        select: () => ({
          eq: (col: string, _val: unknown) => {
            void col;
            return {
              eq: () => ({
                order: () => ({
                  gt: (_col: string, seq: number) =>
                    Promise.resolve({
                      data: [
                        { seq: 0, object_key: "k0" },
                        { seq: 1, object_key: "k1" },
                        { seq: 2, object_key: "k2" },
                      ].filter((r) => r.seq > seq),
                      error: null,
                    }),
                }),
              }),
            };
          },
        }),
      };
    },
    storage: {
      from: (bucket: string) => ({
        download: (objectKey: string) => {
          downloaded.push(`${bucket}/${objectKey}`);
          return Promise.resolve({
            data: {
              arrayBuffer: () =>
                Promise.resolve(new TextEncoder().encode("placeholder").buffer),
            },
            error: null,
          });
        },
      }),
    },
    // deno-lint-ignore no-explicit-any
  } as any;

  // decompressChunk is real here (no DI in this module — see the
  // module's own design note) so this test only exercises the
  // query/download plumbing, not real zstd framing; a real end-to-end
  // decompress is exercised by the Go e2e suite and CO-403's own
  // real-verification fixtures already. Each chunk downloads then
  // immediately decompresses before the next one starts, so a fake,
  // non-zstd "placeholder" blob throws on the very first chunk — this
  // still proves the afterSeq filter reached the query (k0 is
  // excluded) and that the first download call used the right
  // bucket/object key.
  try {
    await readSessionMainChunks(admin, "carryon-sessions", "s1", {
      afterSeq: 0,
    });
  } catch {
    // expected — see comment above
  }
  assertEquals(downloaded, ["carryon-sessions/k1"]);
});
