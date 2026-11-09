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
  callerUpdateRows?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  onUpdate?: (patch: any) => void;
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
                        opts.member ?? { data: null, error: null },
                      ),
                  }),
                }),
              }),
            };
          }
          throw new Error(`unexpected admin table ${table}`);
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
    callerClient: () =>
      ({
        from(table: string) {
          if (table === "sessions") {
            return {
              // deno-lint-ignore no-explicit-any
              update: (patch: any) => {
                opts.onUpdate?.(patch);
                return {
                  eq: () => ({
                    select: () =>
                      Promise.resolve(
                        opts.callerUpdateRows ??
                          { data: [{ id: "s1" }], error: null },
                      ),
                  }),
                };
              },
            };
          }
          throw new Error(`unexpected caller table ${table}`);
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
  };
}

function req(body: unknown): Request {
  return new Request("http://localhost/rename-session", {
    method: "POST",
    headers: {
      Authorization: "Bearer tok",
      "content-type": "application/json",
    },
    body: JSON.stringify(body),
  });
}

const SESSION_ID = "11111111-1111-4111-8111-111111111111";

Deno.test("rejects non-POST", async () => {
  const res = await handle(
    new Request("http://localhost/x", { method: "GET" }),
  );
  assertEquals(res.status, 405);
});

Deno.test("rejects a missing or invalid caller token", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, title: "fix payments" }),
    fakeDeps({ user: null }),
  );
  assertEquals(res.status, 401);
});

Deno.test("rejects an invalid session id", async () => {
  const res = await handle(
    req({ sessionId: "not-a-uuid", title: "fix payments" }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});

Deno.test("rejects an empty title", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, title: "   " }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});

Deno.test("rejects a title over 200 characters", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, title: "x".repeat(201) }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});

Deno.test("404s when the session doesn't exist", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, title: "fix payments" }),
    fakeDeps({ user: fakeUser(), session: { data: null, error: null } }),
  );
  assertEquals(res.status, 404);
});

Deno.test("the owner can rename their own session", async () => {
  let gotPatch: unknown;
  const res = await handle(
    req({ sessionId: SESSION_ID, title: "  fix payments retry  " }),
    fakeDeps({
      user: fakeUser("u1"),
      session: { data: { project_id: "p1", user_id: "u1" }, error: null },
      member: { data: null, error: null }, // not even a project member — ownership alone is enough
      onUpdate: (patch) => {
        gotPatch = patch;
      },
    }),
  );
  assertEquals(res.status, 200);
  // the server trims the title and always stamps title_source manual,
  // regardless of what the laptop sent for either
  assertEquals(gotPatch, {
    title: "fix payments retry",
    title_source: "manual",
  });
});

Deno.test("an admin (not the owner) can rename a session in their project", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, title: "payment architecture decision" }),
    fakeDeps({
      user: fakeUser("admin1"),
      session: { data: { project_id: "p1", user_id: "owner1" }, error: null },
      member: { data: { role: "admin" }, error: null },
    }),
  );
  assertEquals(res.status, 200);
});

Deno.test("a viewer who isn't the owner is forbidden", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, title: "sneaky rename" }),
    fakeDeps({
      user: fakeUser("viewer1"),
      session: { data: { project_id: "p1", user_id: "owner1" }, error: null },
      member: { data: { role: "viewer" }, error: null },
    }),
  );
  assertEquals(res.status, 403);
});

Deno.test("a non-member who isn't the owner is forbidden", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, title: "sneaky rename" }),
    fakeDeps({
      user: fakeUser("stranger1"),
      session: { data: { project_id: "p1", user_id: "owner1" }, error: null },
      member: { data: null, error: null },
    }),
  );
  assertEquals(res.status, 403);
});

Deno.test("a zero-row RLS-denied update surfaces as forbidden, not a false success", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, title: "fix payments" }),
    fakeDeps({
      user: fakeUser("u1"),
      session: { data: { project_id: "p1", user_id: "u1" }, error: null },
      callerUpdateRows: { data: [], error: null },
    }),
  );
  assertEquals(res.status, 403);
});

Deno.test("surfaces a session lookup failure as 500", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, title: "fix payments" }),
    fakeDeps({
      user: fakeUser(),
      session: { data: null, error: { code: "XXXXX" } },
    }),
  );
  assertEquals(res.status, 500);
});

Deno.test("surfaces a membership lookup failure as 500", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, title: "fix payments" }),
    fakeDeps({
      user: fakeUser("stranger1"),
      session: { data: { project_id: "p1", user_id: "owner1" }, error: null },
      member: { data: null, error: { code: "XXXXX" } },
    }),
  );
  assertEquals(res.status, 500);
});

Deno.test("surfaces a session update failure as 500", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, title: "fix payments" }),
    fakeDeps({
      user: fakeUser("u1"),
      session: { data: { project_id: "p1", user_id: "u1" }, error: null },
      callerUpdateRows: { data: null, error: { code: "XXXXX" } },
    }),
  );
  assertEquals(res.status, 500);
});
