import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

const SESSION_ID = "11111111-1111-4111-8111-111111111111";

// deno-lint-ignore no-explicit-any
function fakeUser(id = "caller-1"): any {
  return { id, email: "ali@devminified.com" };
}

function fakeDeps(opts: {
  user?: unknown;
  // deno-lint-ignore no-explicit-any
  session?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  project?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  member?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  owner?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  chunks?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  forks?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  signedUrl?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  audit?: { error?: any };
  onAudit?: (row: unknown) => void;
  onStorageFrom?: (bucket: string) => void;
}): Deps {
  return {
    requireUser: () => Promise.resolve(opts.user as never),
    adminClient: () =>
      ({
        from(table: string) {
          if (table === "sessions") {
            // forks query reuses this same table name — distinguish by
            // which filter method is called next (eq("id", …) vs
            // eq("parent_session_id", …)), mirroring how the real
            // client chains differently for each.
            return {
              select: (cols: string) => ({
                eq: (col: string) => {
                  if (col === "parent_session_id") {
                    return {
                      order: () =>
                        Promise.resolve(
                          opts.forks ?? { data: [], error: null },
                        ),
                    };
                  }
                  void cols;
                  return {
                    maybeSingle: () =>
                      Promise.resolve(
                        opts.session ?? {
                          data: {
                            project_id: "p1",
                            user_id: "owner-1",
                            original_cwd: "/Users/dev/acme-api",
                            branch: "main",
                            status: "ended",
                            outcome: "done",
                            title: "fix payment webhook",
                            summary: "rewrote the retry backoff",
                            files_touched: ["webhook.ts"],
                            parent_session_id: null,
                            total_bytes: 4096,
                            started_at: "2026-01-01T18:00:00Z",
                            last_activity_at: "2026-01-01T18:42:00Z",
                          },
                          error: null,
                        },
                      ),
                  };
                },
              }),
            };
          }
          if (table === "projects") {
            return {
              select: () => ({
                eq: () => ({
                  maybeSingle: () =>
                    Promise.resolve(
                      opts.project ?? {
                        data: { name: "acme-api", bucket: null },
                        error: null,
                      },
                    ),
                }),
              }),
            };
          }
          if (table === "session_chunks") {
            return {
              select: () => ({
                eq: () => ({
                  order: () => ({
                    order: () =>
                      Promise.resolve(
                        opts.chunks ?? {
                          data: [
                            {
                              stream_id: "main",
                              seq: 0,
                              byte_start: 0,
                              byte_end: 100,
                              object_key:
                                "p1/owner-1/s1/main/chunk-0.jsonl.zst",
                              sha256: "c".repeat(64),
                            },
                            {
                              stream_id: "main",
                              seq: 1,
                              byte_start: 100,
                              byte_end: 250,
                              object_key:
                                "p1/owner-1/s1/main/chunk-1.jsonl.zst",
                              sha256: "d".repeat(64),
                            },
                          ],
                          error: null,
                        },
                      ),
                  }),
                }),
              }),
            };
          }
          if (table === "audit_log") {
            return {
              insert: (row: unknown) => {
                opts.onAudit?.(row);
                return Promise.resolve(opts.audit ?? { error: null });
              },
            };
          }
          throw new Error(`unexpected table ${table}`);
        },
        auth: {
          admin: {
            getUserById: () =>
              Promise.resolve(
                opts.owner ?? {
                  data: { user: { user_metadata: { user_name: "ali" } } },
                  error: null,
                },
              ),
          },
        },
        storage: {
          from(bucket: string) {
            opts.onStorageFrom?.(bucket);
            return {
              createSignedUrl: (path: string) =>
                Promise.resolve(
                  opts.signedUrl ?? {
                    data: {
                      signedUrl:
                        `http://kong:8000/storage/v1/object/sign/carryon-sessions/${path}?token=token-for-${path}`,
                    },
                    error: null,
                  },
                ),
            };
          },
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
    callerClient: () =>
      ({
        from(table: string) {
          if (table === "project_members") {
            return {
              select: () => ({
                eq: () => ({
                  eq: () => ({
                    maybeSingle: () =>
                      Promise.resolve(
                        opts.member ??
                          { data: { role: "viewer" }, error: null },
                      ),
                  }),
                }),
              }),
            };
          }
          throw new Error(`unexpected table ${table}`);
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
  };
}

function req(body: unknown, auth = "Bearer token"): Request {
  return new Request("http://local/functions/v1/get-session-transcript", {
    method: "POST",
    headers: { Authorization: auth, "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}

Deno.test("rejects non-POST", async () => {
  const res = await handle(
    new Request("http://local", { method: "GET" }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 405);
});

Deno.test("rejects a missing/invalid caller token", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({ user: null }),
  );
  assertEquals(res.status, 401);
});

Deno.test("rejects an invalid sessionId", async () => {
  const res = await handle(
    req({ sessionId: "not-a-uuid" }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});

Deno.test("returns 404 when the session doesn't exist", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({ user: fakeUser(), session: { data: null, error: null } }),
  );
  assertEquals(res.status, 404);
});

Deno.test("a non-member gets 403", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({ user: fakeUser(), member: { data: null, error: null } }),
  );
  assertEquals(res.status, 403);
});

Deno.test("a viewer gets 200 — the minimum role, unlike get-manifest's resumer+", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      member: { data: { role: "viewer" }, error: null },
    }),
  );
  assertEquals(res.status, 200);
});

Deno.test("returns session metadata for the side panel", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({ user: fakeUser() }),
  );
  const body = await res.json();
  assertEquals(body.session.ownerLogin, "ali");
  assertEquals(body.session.title, "fix payment webhook");
  assertEquals(body.session.summary, "rewrote the retry backoff");
  assertEquals(body.session.outcome, "done");
  assertEquals(body.session.filesTouched, ["webhook.ts"]);
  assertEquals(body.session.projectName, "acme-api");
  assertEquals(body.session.totalBytes, 4096);
});

Deno.test("chunks come back in order with a signed download token each, no raw signed url", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({ user: fakeUser() }),
  );
  const body = await res.json();
  assertEquals(body.chunks.length, 2);
  assertEquals(body.chunks[0].seq, 0);
  assertEquals(body.chunks[1].seq, 1);
  for (const c of body.chunks) {
    if (typeof c.objectKey !== "string" || c.objectKey.length === 0) {
      throw new Error(
        `chunk ${c.seq} missing its objectKey — the browser needs it to build the real download URL`,
      );
    }
    if (typeof c.downloadToken !== "string" || c.downloadToken.length === 0) {
      throw new Error(`chunk ${c.seq} missing a signed downloadToken`);
    }
    if (c.downloadUrl !== undefined) {
      throw new Error(
        `chunk ${c.seq} leaked the Edge Function's own signed url`,
      );
    }
  }
});

Deno.test("CO-305: signs chunk downloads against a project's dedicated bucket", async () => {
  const seen: string[] = [];
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      project: {
        data: { name: "acme-api", bucket: "client-x-bucket" },
        error: null,
      },
      onStorageFrom: (bucket) => seen.push(bucket),
    }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  for (const c of body.chunks) {
    assertEquals(c.bucket, "client-x-bucket");
  }
  if (seen.some((b) => b !== "client-x-bucket")) {
    throw new Error(
      `expected every storage call to use client-x-bucket, got ${seen}`,
    );
  }
});

Deno.test("returns forks sorted oldest first", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      forks: {
        data: [
          {
            id: "fork-1",
            title: "resumed by sara",
            started_at: "2026-01-02T00:00:00Z",
          },
        ],
        error: null,
      },
    }),
  );
  const body = await res.json();
  assertEquals(body.forks, [
    {
      id: "fork-1",
      title: "resumed by sara",
      startedAt: "2026-01-02T00:00:00Z",
    },
  ]);
});

Deno.test("a session with no forks returns an empty array", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({ user: fakeUser() }),
  );
  const body = await res.json();
  assertEquals(body.forks, []);
});

Deno.test("writes a view audit row naming the actor and session", async () => {
  // deno-lint-ignore no-explicit-any
  let audited: any;
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser("caller-9"),
      onAudit: (row) => {
        audited = row;
      },
    }),
  );
  assertEquals(res.status, 200);
  assertEquals(audited.actor, "caller-9");
  assertEquals(audited.action, "view");
  assertEquals(audited.session_id, SESSION_ID);
  assertEquals(audited.project_id, "p1");
});

Deno.test("an audit log write failure doesn't fail the transcript response", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      audit: { error: { code: "XXXXX", message: "boom" } },
    }),
  );
  assertEquals(res.status, 200);
});

Deno.test("surfaces a chunk-signing failure as 500", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      signedUrl: { data: null, error: { message: "sign failed" } },
    }),
  );
  assertEquals(res.status, 500);
});

Deno.test("a session with no chunks yet returns an empty array, not an error", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({ user: fakeUser(), chunks: { data: [], error: null } }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.chunks, []);
});
