import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

// deno-lint-ignore no-explicit-any
function fakeUser(id = "caller-1"): any {
  return { id, email: "ali@devminified.com" };
}

const SESSION_ROWS = [
  {
    id: "11111111-1111-4111-8111-111111111111",
    project_id: "p1",
    user_id: "u-ali",
    branch: "main",
    title: "fix payments retry",
    last_activity_at: "2026-11-05T10:00:00.000Z",
    status: "live",
  },
  {
    id: "22222222-2222-4222-8222-222222222222",
    project_id: "p1",
    user_id: "u-sara",
    branch: "feature/x",
    title: null,
    last_activity_at: "2026-11-04T10:00:00.000Z",
    status: "ended",
  },
];

const LOGIN_BY_USER: Record<string, string> = {
  "u-ali": "ali",
  "u-sara": "sara",
};

function fakeDeps(opts: {
  user?: unknown;
  // deno-lint-ignore no-explicit-any
  memberships?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  projects?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  sessions?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  owner?: Record<string, { data?: any; error?: any }>;
}): Deps {
  return {
    requireUser: () => Promise.resolve(opts.user as never),
    adminClient: () =>
      ({
        from(table: string) {
          if (table === "project_members") {
            return {
              select: () => ({
                eq: () =>
                  Promise.resolve(
                    opts.memberships ??
                      { data: [{ project_id: "p1" }], error: null },
                  ),
              }),
            };
          }
          if (table === "projects") {
            return {
              select: () => ({
                in: () =>
                  Promise.resolve(
                    opts.projects ??
                      { data: [{ id: "p1", name: "acme-api" }], error: null },
                  ),
              }),
            };
          }
          if (table === "sessions") {
            // deno-lint-ignore no-explicit-any
            const builder: any = {
              _status: undefined as string | undefined,
              _since: undefined as string | undefined,
              select() {
                return this;
              },
              in() {
                return this;
              },
              order() {
                return this;
              },
              limit() {
                return this;
              },
              eq(_col: string, val: string) {
                this._status = val;
                return this;
              },
              gte(_col: string, val: string) {
                this._since = val;
                return this;
              },
              then(resolve: (v: unknown) => unknown) {
                if (opts.sessions) {
                  return Promise.resolve(opts.sessions).then(
                    resolve,
                  );
                }
                let data = SESSION_ROWS;
                if (this._status) {
                  data = data.filter((r) => r.status === this._status);
                }
                if (this._since) {
                  data = data.filter((r) => r.last_activity_at >= this._since);
                }
                return Promise.resolve({ data, error: null }).then(resolve);
              },
            };
            return builder;
          }
          throw new Error(`unexpected table ${table}`);
        },
        auth: {
          admin: {
            getUserById: (uid: string) =>
              Promise.resolve(
                opts.owner?.[uid] ?? {
                  data: {
                    user: {
                      user_metadata: { user_name: LOGIN_BY_USER[uid] ?? null },
                    },
                  },
                  error: null,
                },
              ),
          },
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
  };
}

function req(body: unknown, auth = "Bearer token"): Request {
  return new Request("http://local/functions/v1/list-sessions", {
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
  const res = await handle(req({}), fakeDeps({ user: null }));
  assertEquals(res.status, 401);
});

Deno.test("rejects an invalid status value", async () => {
  const res = await handle(
    req({ status: "paused" }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});

Deno.test("a caller with no project memberships gets an empty list, not an error", async () => {
  const res = await handle(
    req({}),
    fakeDeps({ user: fakeUser(), memberships: { data: [], error: null } }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.sessions, []);
});

Deno.test("lists sessions across the caller's accessible projects, newest activity first", async () => {
  const res = await handle(req({}), fakeDeps({ user: fakeUser() }));
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.sessions.length, 2);
  assertEquals(body.sessions[0].id, "11111111-1111-4111-8111-111111111111");
  assertEquals(body.sessions[0].projectName, "acme-api");
  assertEquals(body.sessions[0].ownerLogin, "ali");
  assertEquals(body.sessions[1].id, "22222222-2222-4222-8222-222222222222");
  assertEquals(body.sessions[1].ownerLogin, "sara");
  assertEquals(body.sessions[1].title, null);
});

Deno.test("--status filters to the matching status only", async () => {
  const res = await handle(
    req({ status: "ended" }),
    fakeDeps({ user: fakeUser() }),
  );
  const body = await res.json();
  assertEquals(body.sessions.length, 1);
  assertEquals(body.sessions[0].status, "ended");
});

Deno.test("--since filters out sessions last active before the cutoff", async () => {
  const res = await handle(
    req({ since: "2026-11-05T00:00:00.000Z" }),
    fakeDeps({ user: fakeUser() }),
  );
  const body = await res.json();
  assertEquals(body.sessions.length, 1);
  assertEquals(body.sessions[0].id, "11111111-1111-4111-8111-111111111111");
});

Deno.test("--dev filters to the matching developer's sessions only, case-insensitively", async () => {
  const res = await handle(
    req({ dev: "SARA" }),
    fakeDeps({ user: fakeUser() }),
  );
  const body = await res.json();
  assertEquals(body.sessions.length, 1);
  assertEquals(body.sessions[0].ownerLogin, "sara");
});

Deno.test("--project filters to the matching project only, case-insensitively", async () => {
  const res = await handle(
    req({ project: "ACME-API" }),
    fakeDeps({
      user: fakeUser(),
      projects: { data: [{ id: "p1", name: "acme-api" }], error: null },
    }),
  );
  const body = await res.json();
  assertEquals(body.sessions.length, 2);
});

Deno.test("--project with no matching accessible project returns an empty list, not an error", async () => {
  const res = await handle(
    req({ project: "some-other-repo" }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.sessions, []);
});

Deno.test("filters can combine (status + dev)", async () => {
  const res = await handle(
    req({ status: "ended", dev: "sara" }),
    fakeDeps({ user: fakeUser() }),
  );
  const body = await res.json();
  assertEquals(body.sessions.length, 1);
  assertEquals(body.sessions[0].id, "22222222-2222-4222-8222-222222222222");
});

Deno.test("--limit caps the number of rows returned", async () => {
  const res = await handle(req({ limit: 1 }), fakeDeps({ user: fakeUser() }));
  const body = await res.json();
  assertEquals(body.sessions.length, 1);
  assertEquals(body.sessions[0].id, "11111111-1111-4111-8111-111111111111");
});

Deno.test("surfaces a membership lookup failure as 500", async () => {
  const res = await handle(
    req({}),
    fakeDeps({
      user: fakeUser(),
      memberships: { data: null, error: { code: "XXXXX" } },
    }),
  );
  assertEquals(res.status, 500);
});

Deno.test("surfaces an owner lookup failure as 500", async () => {
  const res = await handle(
    req({}),
    fakeDeps({
      user: fakeUser(),
      owner: { "u-ali": { data: null, error: { message: "boom" } } },
    }),
  );
  assertEquals(res.status, 500);
});
