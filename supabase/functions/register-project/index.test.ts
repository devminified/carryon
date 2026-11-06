import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

// deno-lint-ignore no-explicit-any
function fakeUser(id = "u1"): any {
  return { id, email: "ali@devminified.com" };
}

function fakeDeps(opts: {
  user?: unknown;
  // deno-lint-ignore no-explicit-any
  insertProject?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  insertMember?: { error?: any };
}): Deps {
  return {
    requireUser: () => Promise.resolve(opts.user as never),
    adminClient: () =>
      ({
        from(table: string) {
          if (table === "projects") {
            return {
              insert() {
                return {
                  select() {
                    return {
                      single: () =>
                        Promise.resolve(
                          opts.insertProject ??
                            { data: null, error: { message: "unset" } },
                        ),
                    };
                  },
                };
              },
            };
          }
          if (table === "project_members") {
            return {
              insert: () =>
                Promise.resolve(opts.insertMember ?? { error: null }),
            };
          }
          throw new Error(`unexpected table ${table}`);
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
  };
}

function req(body: unknown, auth = "Bearer token"): Request {
  return new Request("http://local/functions/v1/register-project", {
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
    req({ name: "acme-api", gitRemote: "github.com/devminified/acme-api" }),
    fakeDeps({ user: null }),
  );
  assertEquals(res.status, 401);
});

Deno.test("rejects a non-canonical gitRemote", async () => {
  const res = await handle(
    req({
      name: "acme-api",
      gitRemote: "https://github.com/devminified/acme-api.git",
    }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});

Deno.test("creates the project and its first admin member", async () => {
  const deps = fakeDeps({
    user: fakeUser("u1"),
    insertProject: {
      data: {
        id: "p1",
        name: "acme-api",
        git_remote: "github.com/devminified/acme-api",
      },
      error: null,
    },
    insertMember: { error: null },
  });

  const res = await handle(
    req({ name: "acme-api", gitRemote: "github.com/devminified/acme-api" }),
    deps,
  );
  assertEquals(res.status, 201);
  const body = await res.json();
  assertEquals(body, {
    id: "p1",
    name: "acme-api",
    gitRemote: "github.com/devminified/acme-api",
  });
});

Deno.test("maps a unique-violation on git_remote to 409", async () => {
  const deps = fakeDeps({
    user: fakeUser(),
    insertProject: {
      data: null,
      error: { code: "23505", message: "duplicate key" },
    },
  });

  const res = await handle(
    req({ name: "acme-api", gitRemote: "github.com/devminified/acme-api" }),
    deps,
  );
  assertEquals(res.status, 409);
});

Deno.test("surfaces a failed member insert instead of silently losing it", async () => {
  const deps = fakeDeps({
    user: fakeUser(),
    insertProject: {
      data: {
        id: "p1",
        name: "acme-api",
        git_remote: "github.com/devminified/acme-api",
      },
      error: null,
    },
    insertMember: { error: { code: "XXXXX", message: "boom" } },
  });

  const res = await handle(
    req({ name: "acme-api", gitRemote: "github.com/devminified/acme-api" }),
    deps,
  );
  assertEquals(res.status, 500);
  const body = await res.json();
  assertEquals(body.error, "partial_failure");
});
