import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

const PARENT_ID = "11111111-1111-4111-8111-111111111111";
const NEW_ID = "22222222-2222-4222-8222-222222222222";

// deno-lint-ignore no-explicit-any
function fakeUser(id = "puller-1"): any {
  return { id, email: "bob@devminified.com" };
}

function fakeDeps(opts: {
  user?: unknown;
  // deno-lint-ignore no-explicit-any
  parent?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  member?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  upsert?: { error?: any };
  // deno-lint-ignore no-explicit-any
  onUpsert?: (row: any) => void;
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
                      opts.parent ?? {
                        data: {
                          project_id: "p1",
                          original_cwd: "/Users/ali/acme-api",
                          branch: "main",
                          head_sha: "a".repeat(40),
                          claude_code_version: "2.1.0",
                          machine: "alis-macbook",
                        },
                        error: null,
                      },
                    ),
                }),
              }),
            };
          }
          throw new Error(`unexpected table ${table}`);
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
    // CO-302: the role check and the upsert itself run as the caller.
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
                          { data: { role: "resumer" }, error: null },
                      ),
                  }),
                }),
              }),
            };
          }
          if (table === "sessions") {
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
        // deno-lint-ignore no-explicit-any
      }) as any,
  };
}

function req(body: unknown, auth = "Bearer token"): Request {
  return new Request("http://local/functions/v1/register-fork", {
    method: "POST",
    headers: { Authorization: auth, "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}

function validBody(overrides: Record<string, unknown> = {}) {
  return { parentSessionId: PARENT_ID, newSessionId: NEW_ID, ...overrides };
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

Deno.test("rejects an invalid parentSessionId", async () => {
  const res = await handle(
    req(validBody({ parentSessionId: "not-a-uuid" })),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});

Deno.test("returns 404 when the parent session doesn't exist", async () => {
  const res = await handle(
    req(validBody()),
    fakeDeps({ user: fakeUser(), parent: { data: null, error: null } }),
  );
  assertEquals(res.status, 404);
});

Deno.test("a non-member of the parent's project gets 403", async () => {
  const res = await handle(
    req(validBody()),
    fakeDeps({ user: fakeUser(), member: { data: null, error: null } }),
  );
  assertEquals(res.status, 403);
});

Deno.test("a viewer (below resumer) gets 403", async () => {
  const res = await handle(
    req(validBody()),
    fakeDeps({
      user: fakeUser(),
      member: { data: { role: "viewer" }, error: null },
    }),
  );
  assertEquals(res.status, 403);
});

Deno.test("a resumer succeeds", async () => {
  const res = await handle(
    req(validBody()),
    fakeDeps({
      user: fakeUser(),
      member: { data: { role: "resumer" }, error: null },
    }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.sessionId, NEW_ID);
});

Deno.test("an admin succeeds too", async () => {
  const res = await handle(
    req(validBody()),
    fakeDeps({
      user: fakeUser(),
      member: { data: { role: "admin" }, error: null },
    }),
  );
  assertEquals(res.status, 200);
});

Deno.test("the new row has the correct parent, puller (from the JWT, not a body field), and status", async () => {
  // deno-lint-ignore no-explicit-any
  let upserted: any;
  const res = await handle(
    req(validBody()),
    fakeDeps({
      user: fakeUser("puller-77"),
      onUpsert: (row) => {
        upserted = row;
      },
    }),
  );
  assertEquals(res.status, 200);
  assertEquals(upserted.id, NEW_ID);
  assertEquals(upserted.parent_session_id, PARENT_ID);
  assertEquals(upserted.user_id, "puller-77");
  assertEquals(upserted.status, "live");
  assertEquals(upserted.project_id, "p1");
});

Deno.test("a client-supplied puller field in the body is ignored -- identity always comes from the JWT", async () => {
  // deno-lint-ignore no-explicit-any
  let upserted: any;
  const res = await handle(
    req(validBody({ puller: "someone-else-entirely" })),
    fakeDeps({
      user: fakeUser("real-puller"),
      onUpsert: (row) => {
        upserted = row;
      },
    }),
  );
  assertEquals(res.status, 200);
  assertEquals(upserted.user_id, "real-puller");
});

Deno.test("the fork inherits the parent's cwd/branch/head_sha/version/machine", async () => {
  // deno-lint-ignore no-explicit-any
  let upserted: any;
  const res = await handle(
    req(validBody()),
    fakeDeps({
      user: fakeUser(),
      onUpsert: (row) => {
        upserted = row;
      },
    }),
  );
  assertEquals(res.status, 200);
  assertEquals(upserted.original_cwd, "/Users/ali/acme-api");
  assertEquals(upserted.branch, "main");
  assertEquals(upserted.head_sha, "a".repeat(40));
  assertEquals(upserted.claude_code_version, "2.1.0");
  assertEquals(upserted.machine, "alis-macbook");
});

Deno.test("retrying the same newSessionId is idempotent (I3)", async () => {
  const deps = fakeDeps({ user: fakeUser() });
  const res1 = await handle(req(validBody()), deps);
  const res2 = await handle(req(validBody()), deps);
  assertEquals(res1.status, 200);
  assertEquals(res2.status, 200);
});

Deno.test("pulling the same parent twice with two different newSessionIds succeeds both times", async () => {
  // deno-lint-ignore no-explicit-any
  const upserts: any[] = [];
  const deps = fakeDeps({
    user: fakeUser(),
    onUpsert: (row) => upserts.push(row),
  });
  const res1 = await handle(req(validBody({ newSessionId: NEW_ID })), deps);
  const res2 = await handle(
    req(validBody({ newSessionId: "33333333-3333-4333-8333-333333333333" })),
    deps,
  );
  assertEquals(res1.status, 200);
  assertEquals(res2.status, 200);
  assertEquals(upserts.length, 2);
  if (upserts[0].id === upserts[1].id) {
    throw new Error("two separate pulls produced the same fork id");
  }
});

Deno.test("surfaces an upsert failure as 500", async () => {
  const res = await handle(
    req(validBody()),
    fakeDeps({
      user: fakeUser(),
      upsert: { error: { code: "XXXXX", message: "boom" } },
    }),
  );
  assertEquals(res.status, 500);
});

Deno.test("surfaces a parent lookup failure as 500", async () => {
  const res = await handle(
    req(validBody()),
    fakeDeps({
      user: fakeUser(),
      parent: { data: null, error: { code: "XXXXX" } },
    }),
  );
  assertEquals(res.status, 500);
});
