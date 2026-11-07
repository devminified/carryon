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
  signedUrl?: { data?: any; error?: any };
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
          throw new Error(`unexpected table ${table}`);
        },
        storage: {
          from(bucket: string) {
            if (bucket !== "carryon-sessions") {
              throw new Error(`unexpected bucket ${bucket}`);
            }
            return {
              createSignedUploadUrl: () =>
                Promise.resolve(
                  opts.signedUrl ?? {
                    data: {
                      signedUrl: "http://kong:8000/signed?token=abc",
                      token: "abc",
                      path: "x",
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

function validBody(overrides: Record<string, unknown> = {}) {
  return {
    sessionId: "11111111-1111-4111-8111-111111111111",
    seq: 0,
    ...overrides,
  };
}

function req(body: unknown, auth = "Bearer token"): Request {
  return new Request("http://local/functions/v1/sign-snapshot-bundle", {
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

Deno.test("returns a signed upload token and the derived object key", async () => {
  const deps = fakeDeps({
    user: fakeUser("u1"),
    session: { data: { project_id: "p1", user_id: "u1" }, error: null },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.uploadToken, "abc");
  assertEquals(body.uploadUrl, undefined);
  assertEquals(
    body.objectKey,
    "p1/u1/11111111-1111-4111-8111-111111111111/code-0.bundle",
  );
});

Deno.test("derives the object key from the session's own stored project/user, not the caller's", async () => {
  // The caller (u2) is a different user than the session's owner
  // (user_id "owner-id") — the object key must still be built from the
  // session's own stored identity, matching confirm-chunk's pattern.
  const deps = fakeDeps({
    user: fakeUser("u2"),
    session: { data: { project_id: "p1", user_id: "owner-id" }, error: null },
  });

  const res = await handle(req(validBody({ seq: 3 })), deps);
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(
    body.objectKey,
    "p1/owner-id/11111111-1111-4111-8111-111111111111/code-3.bundle",
  );
});

Deno.test("surfaces a signing failure as 500", async () => {
  const deps = fakeDeps({
    user: fakeUser(),
    signedUrl: { data: null, error: { message: "boom" } },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 500);
});
