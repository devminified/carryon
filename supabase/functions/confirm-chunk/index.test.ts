import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

// deno-lint-ignore no-explicit-any
function fakeUser(id = "u1"): any {
  return { id, email: "ali@devminified.com" };
}

function fakeDeps(opts: {
  user?: unknown;
  // deno-lint-ignore no-explicit-any
  session?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  member?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  info?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  chunkUpsert?: { error?: any };
  // deno-lint-ignore no-explicit-any
  chunkRows?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  sessionUpdate?: { error?: any };
  // deno-lint-ignore no-explicit-any
  onSessionUpdate?: (row: any) => void;
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
                        data: { project_id: "p1", user_id: "u1" },
                        error: null,
                      },
                    ),
                }),
              }),
              // deno-lint-ignore no-explicit-any
              update: (row: any) => {
                opts.onSessionUpdate?.(row);
                return {
                  eq: () =>
                    Promise.resolve(opts.sessionUpdate ?? { error: null }),
                };
              },
            };
          }
          if (table === "project_members") {
            return {
              select: () => ({
                eq: () => ({
                  eq: () => ({
                    maybeSingle: () =>
                      Promise.resolve(
                        opts.member ?? {
                          data: { role: "resumer" },
                          error: null,
                        },
                      ),
                  }),
                }),
              }),
            };
          }
          if (table === "session_chunks") {
            return {
              upsert: () =>
                Promise.resolve(opts.chunkUpsert ?? { error: null }),
              select: () => ({
                eq: () =>
                  Promise.resolve(
                    opts.chunkRows ?? {
                      data: [{ byte_start: 0, byte_end: 100 }],
                      error: null,
                    },
                  ),
              }),
            };
          }
          throw new Error(`unexpected table ${table}`);
        },
        storage: {
          from(bucket: string) {
            if (bucket !== "carryon-sessions") {
              throw new Error(`unexpected bucket ${bucket}`);
            }
            return {
              info: () =>
                Promise.resolve(opts.info ?? { data: {}, error: null }),
            };
          },
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
  };
}

function validBody(overrides: Record<string, unknown> = {}) {
  return {
    sessionId: "11111111-1111-4111-8111-111111111111",
    streamId: "main",
    seq: 0,
    byteStart: 0,
    byteEnd: 100,
    sha256: "a".repeat(64),
    ...overrides,
  };
}

function req(body: unknown, auth = "Bearer token"): Request {
  return new Request("http://local/functions/v1/confirm-chunk", {
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
  const res = await handle(req(validBody()), fakeDeps({ user: null }));
  assertEquals(res.status, 401);
});

Deno.test("rejects byteEnd <= byteStart", async () => {
  const res = await handle(
    req(validBody({ byteStart: 100, byteEnd: 100 })),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});

Deno.test("returns 404 when the session doesn't exist", async () => {
  const res = await handle(
    req(validBody()),
    fakeDeps({ user: fakeUser(), session: { data: null, error: null } }),
  );
  assertEquals(res.status, 404);
});

Deno.test("rejects a caller who isn't a project member", async () => {
  const res = await handle(
    req(validBody()),
    fakeDeps({ user: fakeUser(), member: { data: null, error: null } }),
  );
  assertEquals(res.status, 403);
});

Deno.test("rejects a chunk whose object is missing from storage", async () => {
  const res = await handle(
    req(validBody()),
    fakeDeps({
      user: fakeUser(),
      info: { data: null, error: { code: "NoSuchKey", message: "not found" } },
    }),
  );
  assertEquals(res.status, 409);
  const body = await res.json();
  assertEquals(body.error, "object_missing");
});

Deno.test("inserts the session_chunks row when the object exists", async () => {
  const deps = fakeDeps({
    user: fakeUser("u1"),
    session: { data: { project_id: "p1", user_id: "u1" }, error: null },
    member: { data: { role: "resumer" }, error: null },
    info: { data: { size: 50 }, error: null },
    chunkUpsert: { error: null },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.ok, true);
});

Deno.test("surfaces a session_chunks upsert failure as 500", async () => {
  const deps = fakeDeps({
    user: fakeUser(),
    chunkUpsert: { error: { code: "XXXXX", message: "boom" } },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 500);
});

Deno.test("recomputes total_bytes from session_chunks and bumps last_activity_at", async () => {
  // deno-lint-ignore no-explicit-any
  let updated: any;
  const deps = fakeDeps({
    user: fakeUser("u1"),
    chunkRows: {
      data: [
        { byte_start: 0, byte_end: 100 },
        { byte_start: 100, byte_end: 250 },
      ],
      error: null,
    },
    onSessionUpdate: (row) => {
      updated = row;
    },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 200);
  assertEquals(updated.total_bytes, 250); // (100-0) + (250-100), not just this one chunk
  assertEquals(typeof updated.last_activity_at, "string");
});

Deno.test("sets status back to live and clears ended_reason — a resumed session un-ends itself", async () => {
  // deno-lint-ignore no-explicit-any
  let updated: any;
  const deps = fakeDeps({
    user: fakeUser("u1"),
    onSessionUpdate: (row) => {
      updated = row;
    },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 200);
  assertEquals(updated.status, "live");
  assertEquals(updated.ended_reason, null);
});

Deno.test("surfaces a total_bytes recompute failure as 500", async () => {
  const deps = fakeDeps({
    user: fakeUser("u1"),
    chunkRows: { data: null, error: { code: "XXXXX", message: "boom" } },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 500);
});

Deno.test("surfaces a session activity update failure as 500", async () => {
  const deps = fakeDeps({
    user: fakeUser("u1"),
    sessionUpdate: { error: { code: "XXXXX", message: "boom" } },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 500);
});

Deno.test("retrying the same (session_id, seq) replaces the row instead of erroring (idempotent, NFR-05)", async () => {
  const deps = fakeDeps({
    user: fakeUser("u1"),
    session: { data: { project_id: "p1", user_id: "u1" }, error: null },
    member: { data: { role: "resumer" }, error: null },
    info: { data: { size: 50 }, error: null },
    chunkUpsert: { error: null }, // a real ON CONFLICT ... DO UPDATE never errors on a repeat key
  });

  const res1 = await handle(req(validBody()), deps);
  const res2 = await handle(req(validBody()), deps);
  assertEquals(res1.status, 200);
  assertEquals(res2.status, 200);
});
