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
  upsert?: { error?: any };
  // deno-lint-ignore no-explicit-any
  onUpsert?: (row: any) => void;
  // deno-lint-ignore no-explicit-any
  info?: { data?: any; error?: any };
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
                      opts.session ??
                        {
                          data: { project_id: "p1", user_id: "u1" },
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
          if (table === "code_snapshots") {
            return {
              // deno-lint-ignore no-explicit-any
              upsert: (row: any) => {
                opts.onUpsert?.(row);
                return Promise.resolve(opts.upsert ?? { error: null });
              },
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
    seq: 0,
    headSha: "a".repeat(40),
    wipRef: "refs/carryon/ali/11111111-1111-4111-8111-111111111111",
    treeSha: "b".repeat(40),
    ...overrides,
  };
}

function req(body: unknown, auth = "Bearer token"): Request {
  return new Request("http://local/functions/v1/register-snapshot", {
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

Deno.test("rejects a missing wipRef", async () => {
  const res = await handle(
    req(validBody({ wipRef: "" })),
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

Deno.test("upserts the snapshot row", async () => {
  // deno-lint-ignore no-explicit-any
  let upserted: any;
  const deps = fakeDeps({
    user: fakeUser("u1"),
    onUpsert: (row) => {
      upserted = row;
    },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.ok, true);
  assertEquals(upserted.session_id, "11111111-1111-4111-8111-111111111111");
  assertEquals(upserted.head_sha, "a".repeat(40));
  assertEquals(
    upserted.wip_ref,
    "refs/carryon/ali/11111111-1111-4111-8111-111111111111",
  );
  assertEquals(upserted.tree_sha, "b".repeat(40));
});

Deno.test("bundle path: derives object_key from the session's own stored project/user and verifies it exists", async () => {
  // deno-lint-ignore no-explicit-any
  let upserted: any;
  const deps = fakeDeps({
    user: fakeUser("u1"),
    session: { data: { project_id: "p1", user_id: "owner-id" }, error: null },
    onUpsert: (row) => {
      upserted = row;
    },
  });

  const res = await handle(req(validBody({ wipRef: undefined })), deps);
  assertEquals(res.status, 200);
  assertEquals(upserted.wip_ref, null);
  assertEquals(
    upserted.object_key,
    "p1/owner-id/11111111-1111-4111-8111-111111111111/code-0.bundle",
  );
});

Deno.test("bundle path: rejects when the bundle object is missing from storage", async () => {
  const deps = fakeDeps({
    user: fakeUser("u1"),
    info: { data: null, error: { code: "NoSuchKey", message: "not found" } },
  });

  const res = await handle(req(validBody({ wipRef: undefined })), deps);
  assertEquals(res.status, 409);
  const body = await res.json();
  assertEquals(body.error, "object_missing");
});

Deno.test("ref path never stores an object_key", async () => {
  // deno-lint-ignore no-explicit-any
  let upserted: any;
  const deps = fakeDeps({
    user: fakeUser("u1"),
    onUpsert: (row) => {
      upserted = row;
    },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 200);
  assertEquals(upserted.object_key, null);
});

Deno.test("retrying the same (session_id, seq) replaces the row instead of erroring (idempotent, NFR-05)", async () => {
  const deps = fakeDeps({ user: fakeUser("u1") });

  const res1 = await handle(req(validBody()), deps);
  const res2 = await handle(req(validBody()), deps);
  assertEquals(res1.status, 200);
  assertEquals(res2.status, 200);
});

Deno.test("surfaces an upsert failure as 500", async () => {
  const deps = fakeDeps({
    user: fakeUser("u1"),
    upsert: { error: { code: "XXXXX", message: "boom" } },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 500);
});
