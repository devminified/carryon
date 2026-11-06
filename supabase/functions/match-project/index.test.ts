import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

// deno-lint-ignore no-explicit-any
function fakeUser(id = "u1"): any {
  return { id, email: "ali@devminified.com" };
}

function fakeDeps(opts: {
  user?: unknown;
  // deno-lint-ignore no-explicit-any
  project?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  member?: { data?: any; error?: any };
}): Deps {
  return {
    requireUser: () => Promise.resolve(opts.user as never),
    adminClient: () =>
      ({
        from(table: string) {
          if (table === "projects") {
            return {
              select: () => ({
                eq: () => ({
                  maybeSingle: () =>
                    Promise.resolve(
                      opts.project ?? { data: null, error: null },
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
          throw new Error(`unexpected table ${table}`);
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
  };
}

function req(body: unknown, auth = "Bearer token"): Request {
  return new Request("http://local/functions/v1/match-project", {
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
    req({ gitRemote: "github.com/devminified/acme-api" }),
    fakeDeps({ user: null }),
  );
  assertEquals(res.status, 401);
});

Deno.test("returns 404 for an unregistered remote", async () => {
  const res = await handle(
    req({ gitRemote: "github.com/devminified/unregistered" }),
    fakeDeps({ user: fakeUser(), project: { data: null, error: null } }),
  );
  assertEquals(res.status, 404);
});

Deno.test("returns 403 for a registered remote the caller isn't a member of", async () => {
  const res = await handle(
    req({ gitRemote: "github.com/devminified/acme-api" }),
    fakeDeps({
      user: fakeUser(),
      project: { data: { id: "p1" }, error: null },
      member: { data: null, error: null },
    }),
  );
  assertEquals(res.status, 403);
});

Deno.test("returns the project id and role for a member", async () => {
  const res = await handle(
    req({ gitRemote: "github.com/devminified/acme-api" }),
    fakeDeps({
      user: fakeUser(),
      project: { data: { id: "p1" }, error: null },
      member: { data: { role: "resumer" }, error: null },
    }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body, { projectId: "p1", role: "resumer" });
});
