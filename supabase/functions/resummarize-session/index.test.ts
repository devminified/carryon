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
  now?: Date;
  onMarkerUpdate?: (patch: Record<string, unknown>) => void;
  onSummarize?: (sessionId: string) => void;
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
                          data: {
                            project_id: "p1",
                            last_resummarize_requested_at: null,
                          },
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
                        opts.member ?? { data: { role: "admin" }, error: null },
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
              update: (patch: Record<string, unknown>) => {
                opts.onMarkerUpdate?.(patch);
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
    callAnthropic: (() =>
      Promise.resolve({
        text: "",
        usage: { inputTokens: 0, outputTokens: 0 },
      })) as Deps["callAnthropic"],
    summarizeSession: ((_deps: unknown, sessionId: string) => {
      opts.onSummarize?.(sessionId);
      return Promise.resolve({ status: "summarized" as const });
    }) as Deps["summarizeSession"],
    sleep: () => Promise.resolve(),
    now: () => opts.now ?? new Date("2026-06-15T12:00:00Z"),
  };
}

function req(body: unknown): Request {
  return new Request("http://localhost/resummarize-session", {
    method: "POST",
    headers: {
      Authorization: "Bearer tok",
      "content-type": "application/json",
    },
    body: JSON.stringify(body),
  });
}

const SESSION_ID = "11111111-1111-4111-8111-111111111111";

function withAnthropicKey<T>(fn: () => Promise<T>): Promise<T> {
  const prev = Deno.env.get("ANTHROPIC_API_KEY");
  Deno.env.set("ANTHROPIC_API_KEY", "test-key");
  return fn().finally(() => {
    if (prev === undefined) Deno.env.delete("ANTHROPIC_API_KEY");
    else Deno.env.set("ANTHROPIC_API_KEY", prev);
  });
}

Deno.test("rejects non-POST", async () => {
  const res = await handle(
    new Request("http://localhost/x", { method: "GET" }),
  );
  assertEquals(res.status, 405);
});

Deno.test("rejects a missing or invalid caller token", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({ user: null }),
  );
  assertEquals(res.status, 401);
});

Deno.test("rejects an invalid session id", async () => {
  const res = await handle(
    req({ sessionId: "not-a-uuid" }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});

Deno.test("500s cleanly if ANTHROPIC_API_KEY isn't configured", async () => {
  const prev = Deno.env.get("ANTHROPIC_API_KEY");
  Deno.env.delete("ANTHROPIC_API_KEY");
  try {
    const res = await handle(
      req({ sessionId: SESSION_ID }),
      fakeDeps({ user: fakeUser() }),
    );
    assertEquals(res.status, 500);
  } finally {
    if (prev !== undefined) Deno.env.set("ANTHROPIC_API_KEY", prev);
  }
});

Deno.test("the rate-limit marker is still written (and still enforced) even when ANTHROPIC_API_KEY isn't configured", async () => {
  // The admin check, rate limit, and marker write don't depend on the
  // key at all — only the final summarizeSession call does. Checking
  // the key last (not first) is what makes this true; this test is
  // exactly the scenario a wrong ordering broke in practice (a local/
  // CI environment with no real key configured couldn't exercise the
  // rate limit at all — found via this ticket's own e2e suite, not a
  // unit test, which is why this one is written down explicitly now).
  const prev = Deno.env.get("ANTHROPIC_API_KEY");
  Deno.env.delete("ANTHROPIC_API_KEY");
  try {
    // deno-lint-ignore no-explicit-any
    let markerPatch: any = null;
    const deps = fakeDeps({
      user: fakeUser("admin1"),
      onMarkerUpdate: (patch) => {
        markerPatch = patch;
      },
    });

    const first = await handle(req({ sessionId: SESSION_ID }), deps);
    assertEquals(first.status, 500);
    assertEquals(
      markerPatch?.last_resummarize_requested_at,
      "2026-06-15T12:00:00.000Z",
    );

    const second = await handle(
      req({ sessionId: SESSION_ID }),
      fakeDeps({
        user: fakeUser("admin1"),
        session: {
          data: {
            project_id: "p1",
            last_resummarize_requested_at: "2026-06-15T12:00:00.000Z",
          },
          error: null,
        },
      }),
    );
    assertEquals(second.status, 429);
  } finally {
    if (prev !== undefined) Deno.env.set("ANTHROPIC_API_KEY", prev);
  }
});

Deno.test("404s when the session doesn't exist", async () => {
  await withAnthropicKey(async () => {
    const res = await handle(
      req({ sessionId: SESSION_ID }),
      fakeDeps({ user: fakeUser(), session: { data: null, error: null } }),
    );
    assertEquals(res.status, 404);
  });
});

Deno.test("a non-admin is forbidden", async () => {
  await withAnthropicKey(async () => {
    const res = await handle(
      req({ sessionId: SESSION_ID }),
      fakeDeps({
        user: fakeUser("viewer1"),
        member: { data: { role: "viewer" }, error: null },
      }),
    );
    assertEquals(res.status, 403);
  });
});

Deno.test("a non-member is forbidden", async () => {
  await withAnthropicKey(async () => {
    const res = await handle(
      req({ sessionId: SESSION_ID }),
      fakeDeps({
        user: fakeUser("stranger1"),
        member: { data: null, error: null },
      }),
    );
    assertEquals(res.status, 403);
  });
});

Deno.test("an admin triggers a real resummarize when never run before", async () => {
  await withAnthropicKey(async () => {
    let summarizedId: string | null = null;
    // deno-lint-ignore no-explicit-any
    let markerPatch: any = null;
    const res = await handle(
      req({ sessionId: SESSION_ID }),
      fakeDeps({
        user: fakeUser("admin1"),
        onSummarize: (id) => {
          summarizedId = id;
        },
        onMarkerUpdate: (patch) => {
          markerPatch = patch;
        },
      }),
    );
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body.outcome, "summarized");
    assertEquals(summarizedId, SESSION_ID);
    assertEquals(
      markerPatch?.last_resummarize_requested_at,
      "2026-06-15T12:00:00.000Z",
    );
  });
});

Deno.test("rate-limited: rejects a second request within 60 seconds, without calling summarizeSession again", async () => {
  await withAnthropicKey(async () => {
    let summarizeCalls = 0;
    const res = await handle(
      req({ sessionId: SESSION_ID }),
      fakeDeps({
        user: fakeUser("admin1"),
        session: {
          data: {
            project_id: "p1",
            last_resummarize_requested_at: "2026-06-15T11:59:30Z", // 30s ago
          },
          error: null,
        },
        now: new Date("2026-06-15T12:00:00Z"),
        onSummarize: () => {
          summarizeCalls++;
        },
      }),
    );
    assertEquals(res.status, 429);
    const body = await res.json();
    assertEquals(body.error, "rate_limited");
    assertEquals(body.retryAfterSeconds, 30);
    assertEquals(summarizeCalls, 0);
  });
});

Deno.test("allows a new request once 60 seconds have fully elapsed", async () => {
  await withAnthropicKey(async () => {
    const res = await handle(
      req({ sessionId: SESSION_ID }),
      fakeDeps({
        user: fakeUser("admin1"),
        session: {
          data: {
            project_id: "p1",
            last_resummarize_requested_at: "2026-06-15T11:59:00Z", // exactly 60s ago
          },
          error: null,
        },
        now: new Date("2026-06-15T12:00:00Z"),
      }),
    );
    assertEquals(res.status, 200);
  });
});

Deno.test("a zero-row RLS-denied marker update surfaces as forbidden", async () => {
  await withAnthropicKey(async () => {
    const res = await handle(
      req({ sessionId: SESSION_ID }),
      fakeDeps({
        user: fakeUser("admin1"),
        callerUpdateRows: { data: [], error: null },
      }),
    );
    assertEquals(res.status, 403);
  });
});

Deno.test("surfaces a session lookup failure as 500", async () => {
  await withAnthropicKey(async () => {
    const res = await handle(
      req({ sessionId: SESSION_ID }),
      fakeDeps({
        user: fakeUser(),
        session: { data: null, error: { code: "XXXXX" } },
      }),
    );
    assertEquals(res.status, 500);
  });
});

Deno.test("surfaces a membership lookup failure as 500", async () => {
  await withAnthropicKey(async () => {
    const res = await handle(
      req({ sessionId: SESSION_ID }),
      fakeDeps({
        user: fakeUser("stranger1"),
        member: { data: null, error: { code: "XXXXX" } },
      }),
    );
    assertEquals(res.status, 500);
  });
});

Deno.test("surfaces a marker update failure as 500", async () => {
  await withAnthropicKey(async () => {
    const res = await handle(
      req({ sessionId: SESSION_ID }),
      fakeDeps({
        user: fakeUser("admin1"),
        callerUpdateRows: { data: null, error: { code: "XXXXX" } },
      }),
    );
    assertEquals(res.status, 500);
  });
});
