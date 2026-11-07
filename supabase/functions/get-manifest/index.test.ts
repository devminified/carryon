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
  snapshot?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  signedUrl?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  audit?: { error?: any };
  onAudit?: (row: unknown) => void;
}): Deps {
  return {
    requireUser: () => Promise.resolve(opts.user as never),
    adminClient: () =>
      ({
        from(table: string) {
          if (table === "sessions") {
            return {
              select: () => ({
                eq: () => ({
                  maybeSingle: () =>
                    Promise.resolve(
                      opts.session ?? {
                        data: {
                          project_id: "p1",
                          user_id: "owner-1",
                          original_cwd: "/Users/dev/acme-api",
                          branch: "main",
                          head_sha: "a".repeat(40),
                          claude_code_version: "2.1.0",
                          status: "live",
                          last_activity_at: "2026-01-01T18:42:00Z",
                        },
                        error: null,
                      },
                    ),
                }),
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
                        data: {
                          name: "acme-api",
                          git_remote: "github.com/acme/api",
                        },
                        error: null,
                      },
                    ),
                }),
              }),
            };
          }
          if (table === "project_members") {
            return {
              select: () => ({
                eq: () => ({
                  eq: () => ({
                    maybeSingle: () =>
                      Promise.resolve(
                        opts.member ??
                          { data: { role: "resumer" }, error: null },
                      ),
                  }),
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
          if (table === "code_snapshots") {
            return {
              select: () => ({
                eq: () => ({
                  order: () => ({
                    limit: () => ({
                      maybeSingle: () =>
                        Promise.resolve(
                          opts.snapshot ?? {
                            data: {
                              seq: 2,
                              head_sha: "a".repeat(40),
                              tree_sha: "b".repeat(40),
                              wip_ref: "refs/carryon/ali/s1",
                              object_key: null,
                            },
                            error: null,
                          },
                        ),
                    }),
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
            if (bucket !== "carryon-sessions") {
              throw new Error(`unexpected bucket ${bucket}`);
            }
            return {
              createSignedUrl: (path: string) =>
                Promise.resolve(
                  opts.signedUrl ?? {
                    // Deliberately host-shaped like the real local
                    // instance's internal "kong:8000" URL (see
                    // index.ts's signDownloadToken) — if the handler ever
                    // regresses to returning signed.signedUrl as-is
                    // instead of extracting the token, these tests would
                    // see this unreachable-looking host in the response.
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
  };
}

function req(body: unknown, auth = "Bearer token"): Request {
  return new Request("http://local/functions/v1/get-manifest", {
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

Deno.test("a member below resumer gets 403", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      member: { data: { role: "viewer" }, error: null },
    }),
  );
  assertEquals(res.status, 403);
});

Deno.test("a resumer gets 200", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      member: { data: { role: "resumer" }, error: null },
    }),
  );
  assertEquals(res.status, 200);
});

Deno.test("an admin gets 200 too", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      member: { data: { role: "admin" }, error: null },
    }),
  );
  assertEquals(res.status, 200);
});

Deno.test("returns session metadata including the owner's github login", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({ user: fakeUser() }),
  );
  const body = await res.json();
  assertEquals(body.session.ownerLogin, "ali");
  assertEquals(body.session.originalCwd, "/Users/dev/acme-api");
  assertEquals(body.session.branch, "main");
  assertEquals(body.session.headSha, "a".repeat(40));
  assertEquals(body.session.claudeCodeVersion, "2.1.0");
  assertEquals(body.session.status, "live");
  assertEquals(body.session.projectName, "acme-api");
  assertEquals(body.session.gitRemote, "github.com/acme/api");
  assertEquals(body.session.lastActivityAt, "2026-01-01T18:42:00Z");
});

Deno.test("surfaces a project lookup failure as 500", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      project: { data: null, error: { code: "XXXXX", message: "boom" } },
    }),
  );
  assertEquals(res.status, 500);
});

Deno.test("chunks come back in order with a signed download token each, no raw signed url", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({ user: fakeUser() }),
  );
  const body = await res.json();
  assertEquals(body.chunks.length, 2);
  assertEquals(body.chunks[0].seq, 0);
  assertEquals(body.chunks[0].byteStart, 0);
  assertEquals(body.chunks[0].byteEnd, 100);
  assertEquals(body.chunks[1].seq, 1);
  assertEquals(body.chunks[1].byteStart, 100);
  assertEquals(body.chunks[1].byteEnd, 250);
  for (const c of body.chunks) {
    if (typeof c.objectKey !== "string" || c.objectKey.length === 0) {
      throw new Error(`chunk ${c.seq} missing its objectKey`);
    }
    if (typeof c.downloadToken !== "string" || c.downloadToken.length === 0) {
      throw new Error(`chunk ${c.seq} missing a signed downloadToken`);
    }
    if (c.downloadUrl !== undefined) {
      throw new Error(
        `chunk ${c.seq} leaked the Edge Function's own signed url (host-baked, unreachable from the laptop)`,
      );
    }
  }
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

Deno.test("a ref-path snapshot is returned with wipRef, no object key or token", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({ user: fakeUser() }),
  );
  const body = await res.json();
  assertEquals(body.snapshot.wipRef, "refs/carryon/ali/s1");
  assertEquals(body.snapshot.objectKey, undefined);
  assertEquals(body.snapshot.downloadToken, undefined);
  assertEquals(body.snapshot.headSha, "a".repeat(40));
  assertEquals(body.snapshot.treeSha, "b".repeat(40));
});

Deno.test("a bundle-path snapshot is returned with its object key and a signed download token, no wipRef", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      snapshot: {
        data: {
          seq: 2,
          head_sha: "a".repeat(40),
          tree_sha: "b".repeat(40),
          wip_ref: null,
          object_key: "p1/owner-1/s1/code-2.bundle",
        },
        error: null,
      },
    }),
  );
  const body = await res.json();
  assertEquals(body.snapshot.wipRef, undefined);
  assertEquals(body.snapshot.objectKey, "p1/owner-1/s1/code-2.bundle");
  if (
    typeof body.snapshot.downloadToken !== "string" ||
    body.snapshot.downloadToken.length === 0
  ) {
    throw new Error("bundle snapshot missing a signed downloadToken");
  }
});

Deno.test("a session with no snapshot yet returns snapshot: null", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({ user: fakeUser(), snapshot: { data: null, error: null } }),
  );
  const body = await res.json();
  assertEquals(body.snapshot, null);
});

Deno.test("writes a pull_manifest audit row naming the actor and session", async () => {
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
  assertEquals(audited.action, "pull_manifest");
  assertEquals(audited.session_id, SESSION_ID);
});

Deno.test("an audit log write failure doesn't fail the manifest response", async () => {
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
