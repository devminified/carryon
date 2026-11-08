import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

// deno-lint-ignore no-explicit-any
function fakeUser(id = "u1"): any {
  return { id, email: "ali@devminified.com" };
}

function fakeDeps(opts: {
  user?: unknown;
  // deno-lint-ignore no-explicit-any
  config?: { data?: any; error?: any };
}): Deps {
  return {
    requireUser: () => Promise.resolve(opts.user as never),
    adminClient: () =>
      ({
        from(table: string) {
          if (table === "redaction_config") {
            return {
              select: () => ({
                eq: () => ({
                  maybeSingle: () =>
                    Promise.resolve(
                      opts.config ?? {
                        data: {
                          disabled_rule_ids: [],
                          denylist_additions: [],
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
  };
}

function req(auth = "Bearer token"): Request {
  return new Request("http://local/functions/v1/get-config", {
    method: "POST",
    headers: { Authorization: auth, "content-type": "application/json" },
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
  const res = await handle(req(), fakeDeps({ user: null }));
  assertEquals(res.status, 401);
});

Deno.test("returns the configured disabled rule IDs and denylist additions", async () => {
  const res = await handle(
    req(),
    fakeDeps({
      user: fakeUser(),
      config: {
        data: {
          disabled_rule_ids: ["generic-api-key"],
          denylist_additions: ["*.secret"],
        },
        error: null,
      },
    }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.disabledRuleIds, ["generic-api-key"]);
  assertEquals(body.denylistAdditions, ["*.secret"]);
});

Deno.test("returns empty arrays and a null pinnedVersion when no config row exists yet", async () => {
  const res = await handle(
    req(),
    fakeDeps({ user: fakeUser(), config: { data: null, error: null } }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.disabledRuleIds, []);
  assertEquals(body.denylistAdditions, []);
  assertEquals(body.pinnedVersion, null);
});

Deno.test("CO-308: returns the configured pinned version", async () => {
  const res = await handle(
    req(),
    fakeDeps({
      user: fakeUser(),
      config: {
        data: {
          disabled_rule_ids: [],
          denylist_additions: [],
          pinned_version: "1.5.0",
        },
        error: null,
      },
    }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.pinnedVersion, "1.5.0");
});

Deno.test("surfaces a lookup failure as 500", async () => {
  const res = await handle(
    req(),
    fakeDeps({
      user: fakeUser(),
      config: { data: null, error: { code: "XXXXX", message: "boom" } },
    }),
  );
  assertEquals(res.status, 500);
});

Deno.test("any authenticated user gets the same config — no project/role check", async () => {
  const res1 = await handle(req(), fakeDeps({ user: fakeUser("u1") }));
  const res2 = await handle(req(), fakeDeps({ user: fakeUser("u2") }));
  assertEquals(res1.status, 200);
  assertEquals(res2.status, 200);
});
