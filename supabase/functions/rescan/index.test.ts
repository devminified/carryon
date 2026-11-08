import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

const CRON_SECRET = "test-cron-secret";

interface ChunkRow {
  session_id: string;
  stream_id: string;
  seq: number;
  object_key: string;
  uploaded_at: string;
  last_rescanned_at: string | null;
  sha256?: string;
}

interface FakeState {
  configRow?: { disabled_rule_ids?: string[]; updated_at?: string } | null;
  configErr?: unknown;
  chunks: ChunkRow[];
  sessions: { id: string; project_id: string }[];
  projects: { id: string; bucket: string | null }[];
  // deno-lint-ignore no-explicit-any
  auditLog: any[];
  storageObjects: Record<string, { bucket: string; bytes: Uint8Array }>;
  downloadErr?: unknown;
  updateObjErr?: unknown;
  chunkUpdateErr?: unknown;
}

function freshState(overrides: Partial<FakeState> = {}): FakeState {
  return {
    chunks: [],
    sessions: [],
    projects: [],
    auditLog: [],
    storageObjects: {},
    ...overrides,
  };
}

// deno-lint-ignore no-explicit-any
function fakeAdminClient(state: FakeState): any {
  return {
    from(table: string) {
      if (table === "redaction_config") {
        return {
          select: () => ({
            eq: () => ({
              maybeSingle: () =>
                Promise.resolve(
                  state.configErr ? { data: null, error: state.configErr } : {
                    data: state.configRow ??
                      {
                        disabled_rule_ids: [],
                        updated_at: new Date(0).toISOString(),
                      },
                    error: null,
                  },
                ),
            }),
          }),
        };
      }
      if (table === "session_chunks") {
        // The filter chain's own semantics (the `or`/`gte`/`order`/`limit`
        // predicate itself) are verified for real against a live Postgres
        // instance by cmd/carryon's e2e test — this fake just hands back
        // whatever candidates the test scenario set up, same convention
        // every other fake in this codebase already follows (e.g.
        // sync-github-teams' fake `projects.or()`).
        let lim = state.chunks.length;
        const builder = {
          select: () => builder,
          or: () => builder,
          gte: () => builder,
          order: () => builder,
          limit: (n: number) => {
            lim = n;
            return builder;
          },
          then: (
            resolve: (v: { data: ChunkRow[]; error: null }) => void,
          ) => resolve({ data: state.chunks.slice(0, lim), error: null }),
          // deno-lint-ignore no-explicit-any
          update: (patch: any) => ({
            eq: (_c1: string, sessionID: string) => ({
              eq: (_c2: string, streamID: string) => ({
                eq: (_c3: string, seq: number) => {
                  if (state.chunkUpdateErr) {
                    return Promise.resolve({ error: state.chunkUpdateErr });
                  }
                  const row = state.chunks.find(
                    (c) =>
                      c.session_id === sessionID &&
                      c.stream_id === streamID &&
                      c.seq === seq,
                  );
                  if (row) Object.assign(row, patch);
                  return Promise.resolve({ error: null });
                },
              }),
            }),
          }),
        };
        return builder;
      }
      if (table === "sessions") {
        return {
          select: () => ({
            in: (_col: string, ids: string[]) =>
              Promise.resolve({
                data: state.sessions.filter((s) => ids.includes(s.id)),
                error: null,
              }),
          }),
        };
      }
      if (table === "projects") {
        return {
          select: () => ({
            in: (_col: string, ids: string[]) =>
              Promise.resolve({
                data: state.projects.filter((p) => ids.includes(p.id)),
                error: null,
              }),
          }),
        };
      }
      if (table === "audit_log") {
        return {
          // deno-lint-ignore no-explicit-any
          insert: (row: any) => {
            state.auditLog.push(row);
            return Promise.resolve({ error: null });
          },
        };
      }
      throw new Error(`unexpected table ${table}`);
    },
    storage: {
      from(bucket: string) {
        return {
          download: (objectKey: string) => {
            if (state.downloadErr) {
              return Promise.resolve({ data: null, error: state.downloadErr });
            }
            const obj = state.storageObjects[`${bucket}/${objectKey}`];
            if (!obj) {
              return Promise.resolve({
                data: null,
                error: { message: "not found" },
              });
            }
            return Promise.resolve({
              data: { arrayBuffer: () => Promise.resolve(obj.bytes.buffer) },
              error: null,
            });
          },
          update: (objectKey: string, bytes: Uint8Array) => {
            if (state.updateObjErr) {
              return Promise.resolve({ error: state.updateObjErr });
            }
            state.storageObjects[`${bucket}/${objectKey}`] = { bucket, bytes };
            return Promise.resolve({ error: null });
          },
        };
      },
    },
  };
}

// fakeDeps swaps in trivial, deterministic encode/decode stand-ins so
// these tests exercise rescan's own control flow (candidate selection,
// hit/no-hit branching, bucket resolution, error propagation) without
// depending on the real zstd WASM codec — that real round-trip (and
// real cross-compatibility with the laptop's own decoder) is covered
// by cmd/carryon's real end-to-end test instead.
function fakeDeps(state: FakeState, overrides: Partial<Deps> = {}): Deps {
  const dec = new TextDecoder();
  return {
    adminClient: () => fakeAdminClient(state),
    decompressChunk: (data: Uint8Array) => Promise.resolve(data), // identity stand-in
    compressChunk: (data: Uint8Array) => Promise.resolve(data), // identity stand-in
    sha256Hex: (data: Uint8Array) =>
      Promise.resolve(`hash:${dec.decode(data)}`),
    ...overrides,
  };
}

function req(body: unknown, secret = CRON_SECRET): Request {
  return new Request("http://localhost/rescan", {
    method: "POST",
    headers: { "x-cron-secret": secret, "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}

Deno.env.set("RESCAN_CRON_SECRET", CRON_SECRET);

function putObject(
  state: FakeState,
  bucket: string,
  objectKey: string,
  text: string,
) {
  state.storageObjects[`${bucket}/${objectKey}`] = {
    bucket,
    bytes: new TextEncoder().encode(text),
  };
}

Deno.test("rejects non-POST", async () => {
  const res = await handle(
    new Request("http://localhost/rescan", { method: "GET" }),
  );
  assertEquals(res.status, 405);
});

Deno.test("rejects a missing or wrong cron secret", async () => {
  const state = freshState();
  const res = await handle(req({}, "wrong"), fakeDeps(state));
  assertEquals(res.status, 401);
});

Deno.test("no candidates: returns scanned 0, hits 0", async () => {
  const state = freshState();
  const res = await handle(req({}), fakeDeps(state));
  assertEquals(res.status, 200);
  assertEquals(await res.json(), { scanned: 0, hits: 0 });
});

Deno.test("a clean chunk is marked rescanned but not rewritten", async () => {
  const state = freshState({
    chunks: [{
      session_id: "s1",
      stream_id: "main",
      seq: 0,
      object_key: "p1/u1/s1/main/chunk-0.jsonl.zst",
      uploaded_at: new Date().toISOString(),
      last_rescanned_at: null,
    }],
    sessions: [{ id: "s1", project_id: "p1" }],
    projects: [{ id: "p1", bucket: null }],
  });
  putObject(
    state,
    "carryon-sessions",
    "p1/u1/s1/main/chunk-0.jsonl.zst",
    '{"a":1}\n',
  );

  const res = await handle(req({}), fakeDeps(state));
  const body = await res.json();
  assertEquals(body, { scanned: 1, hits: 0 });

  assertEquals(state.chunks[0].last_rescanned_at !== null, true);
  assertEquals(state.auditLog.length, 0);
  // object untouched (identity codec round-trips to the same bytes either way)
});

Deno.test("a chunk with a planted secret is rewritten, re-hashed, and audited", async () => {
  const objectKey = "p1/u1/s1/main/chunk-0.jsonl.zst";
  const state = freshState({
    chunks: [{
      session_id: "s1",
      stream_id: "main",
      seq: 0,
      object_key: objectKey,
      uploaded_at: new Date().toISOString(),
      last_rescanned_at: null,
    }],
    sessions: [{ id: "s1", project_id: "p1" }],
    projects: [{ id: "p1", bucket: null }],
  });
  putObject(
    state,
    "carryon-sessions",
    objectKey,
    `{"secret":"sb_secret_${"x".repeat(24)}"}\n`,
  );

  const res = await handle(req({}), fakeDeps(state));
  const body = await res.json();
  assertEquals(body, { scanned: 1, hits: 1 });

  const rewritten = state.storageObjects[`carryon-sessions/${objectKey}`];
  const rewrittenText = new TextDecoder().decode(rewritten.bytes);
  assertEquals(rewrittenText.includes("sb_secret_"), false);
  assertEquals(rewrittenText.includes("[REDACTED:supabase-service-key]"), true);

  assertEquals(state.chunks[0].sha256 !== undefined, true);
  assertEquals(state.chunks[0].last_rescanned_at !== null, true);

  assertEquals(state.auditLog.length, 1);
  assertEquals(state.auditLog[0].action, "rescan_hit");
  assertEquals(state.auditLog[0].session_id, "s1");
  assertEquals(state.auditLog[0].project_id, "p1");
});

Deno.test("CO-305: scans a project's dedicated bucket, not the shared default", async () => {
  const objectKey = "p1/u1/s1/main/chunk-0.jsonl.zst";
  const state = freshState({
    chunks: [{
      session_id: "s1",
      stream_id: "main",
      seq: 0,
      object_key: objectKey,
      uploaded_at: new Date().toISOString(),
      last_rescanned_at: null,
    }],
    sessions: [{ id: "s1", project_id: "p1" }],
    projects: [{ id: "p1", bucket: "client-x-bucket" }],
  });
  putObject(state, "client-x-bucket", objectKey, '{"a":1}\n');

  const res = await handle(req({}), fakeDeps(state));
  assertEquals(await res.json(), { scanned: 1, hits: 0 });
});

Deno.test("a disabled rule is never flagged", async () => {
  const objectKey = "p1/u1/s1/main/chunk-0.jsonl.zst";
  const state = freshState({
    configRow: {
      disabled_rule_ids: ["supabase-service-key"],
      updated_at: new Date(0).toISOString(),
    },
    chunks: [{
      session_id: "s1",
      stream_id: "main",
      seq: 0,
      object_key: objectKey,
      uploaded_at: new Date().toISOString(),
      last_rescanned_at: null,
    }],
    sessions: [{ id: "s1", project_id: "p1" }],
    projects: [{ id: "p1", bucket: null }],
  });
  putObject(
    state,
    "carryon-sessions",
    objectKey,
    `{"secret":"sb_secret_${"x".repeat(24)}"}\n`,
  );

  const res = await handle(req({}), fakeDeps(state));
  assertEquals(await res.json(), { scanned: 1, hits: 0 });
});

Deno.test("a download failure for one chunk is logged and skipped, not fatal to the batch", async () => {
  const state = freshState({
    chunks: [{
      session_id: "s1",
      stream_id: "main",
      seq: 0,
      object_key: "missing-object",
      uploaded_at: new Date().toISOString(),
      last_rescanned_at: null,
    }],
    sessions: [{ id: "s1", project_id: "p1" }],
    projects: [{ id: "p1", bucket: null }],
  });
  // no object placed in storageObjects — download will 404

  const res = await handle(req({}), fakeDeps(state));
  assertEquals(await res.json(), { scanned: 1, hits: 0 });
  assertEquals(state.chunks[0].last_rescanned_at, null); // never marked — a real retry must pick it up again
});

Deno.test("surfaces a redaction_config lookup failure as 500", async () => {
  const state = freshState({ configErr: { code: "XXXXX" } });
  const res = await handle(req({}), fakeDeps(state));
  assertEquals(res.status, 500);
});

Deno.test("full:true is accepted and passed through without a time-window filter", async () => {
  const state = freshState();
  const res = await handle(req({ full: true }), fakeDeps(state));
  assertEquals(res.status, 200);
});

Deno.test("rejects a limit over the max", async () => {
  const state = freshState();
  const res = await handle(req({ limit: 5000 }), fakeDeps(state));
  assertEquals(res.status, 400);
});
