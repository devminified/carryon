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
  member?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  audit?: { error?: any };
  // deno-lint-ignore no-explicit-any
  onAudit?: (row: any) => void;
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
                        { data: { project_id: "p1" }, error: null },
                    ),
                }),
              }),
            };
          }
          if (table === "audit_log") {
            return {
              // deno-lint-ignore no-explicit-any
              insert: (row: any) => {
                opts.onAudit?.(row);
                return Promise.resolve(opts.audit ?? { error: null });
              },
            };
          }
          throw new Error(`unexpected table ${table}`);
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
                          { data: { role: "resumer" }, error: null },
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
  return new Request("http://local/functions/v1/confirm-pull", {
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

Deno.test("a viewer (below resumer) gets 403", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      member: { data: { role: "viewer" }, error: null },
    }),
  );
  assertEquals(res.status, 403);
});

Deno.test("a resumer succeeds and writes a pull_complete audit row", async () => {
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
  const body = await res.json();
  assertEquals(body.ok, true);
  assertEquals(audited.actor, "caller-9");
  assertEquals(audited.action, "pull_complete");
  assertEquals(audited.session_id, SESSION_ID);
  assertEquals(audited.project_id, "p1");
});

Deno.test("an admin succeeds too", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      member: { data: { role: "admin" }, error: null },
    }),
  );
  assertEquals(res.status, 200);
});

Deno.test("surfaces a session lookup failure as 500", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      session: { data: null, error: { code: "XXXXX", message: "boom" } },
    }),
  );
  assertEquals(res.status, 500);
});

Deno.test("surfaces a membership lookup failure as 500", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      member: { data: null, error: { code: "XXXXX", message: "boom" } },
    }),
  );
  assertEquals(res.status, 500);
});

Deno.test("surfaces an audit log insert failure as 500", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({
      user: fakeUser(),
      audit: { error: { code: "XXXXX", message: "boom" } },
    }),
  );
  assertEquals(res.status, 500);
});
