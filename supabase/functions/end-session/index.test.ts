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
  // CO-302: the update itself now runs as the caller, gated by RLS's
  // owner-only update policy. isOwner stands in for what that policy
  // would decide — true means the update "matches" (one row back),
  // false means it silently matches zero rows, same as a real
  // non-owner's request would.
  isOwner?: boolean;
  // deno-lint-ignore no-explicit-any
  update?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  onUpdate?: (row: any) => void;
}): Deps {
  const isOwner = opts.isOwner ?? true;
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
                        data: { id: "11111111-1111-4111-8111-111111111111" },
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
    callerClient: () =>
      ({
        from(table: string) {
          if (table === "sessions") {
            return {
              // deno-lint-ignore no-explicit-any
              update: (row: any) => {
                opts.onUpdate?.(row);
                return {
                  eq: () => ({
                    select: () =>
                      Promise.resolve(
                        opts.update ??
                          (isOwner
                            ? {
                              data: [{
                                id: "11111111-1111-4111-8111-111111111111",
                              }],
                              error: null,
                            }
                            : { data: [], error: null }),
                      ),
                  }),
                };
              },
            };
          }
          throw new Error(`unexpected table ${table}`);
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
  };
}

function validBody(overrides: Record<string, unknown> = {}) {
  return {
    sessionId: "11111111-1111-4111-8111-111111111111",
    ...overrides,
  };
}

function req(body: unknown, auth = "Bearer token"): Request {
  return new Request("http://local/functions/v1/end-session", {
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

Deno.test("rejects a non-uuid sessionId", async () => {
  const res = await handle(
    req(validBody({ sessionId: "not-a-uuid" })),
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

Deno.test("rejects a caller who isn't the session's owner (RLS matches zero rows)", async () => {
  const res = await handle(
    req(validBody()),
    fakeDeps({ user: fakeUser(), isOwner: false }),
  );
  assertEquals(res.status, 403);
});

Deno.test("sets status=ended, ended_reason=explicit", async () => {
  // deno-lint-ignore no-explicit-any
  let updated: any;
  const deps = fakeDeps({
    user: fakeUser("u1"),
    onUpdate: (row) => {
      updated = row;
    },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.ok, true);
  assertEquals(updated.status, "ended");
  assertEquals(updated.ended_reason, "explicit");
});

Deno.test("calling it twice is a safe no-op (idempotent, I3)", async () => {
  const deps = fakeDeps({ user: fakeUser("u1") });

  const res1 = await handle(req(validBody()), deps);
  const res2 = await handle(req(validBody()), deps);
  assertEquals(res1.status, 200);
  assertEquals(res2.status, 200);
});

Deno.test("surfaces a session lookup failure as 500", async () => {
  const deps = fakeDeps({
    user: fakeUser(),
    session: { data: null, error: { code: "XXXXX", message: "boom" } },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 500);
});

Deno.test("surfaces an update failure as 500", async () => {
  const deps = fakeDeps({
    user: fakeUser(),
    update: { data: null, error: { code: "XXXXX", message: "boom" } },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 500);
});
