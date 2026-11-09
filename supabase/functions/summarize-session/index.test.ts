import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

function fakeDeps(opts: {
  liveSessionIds?: string[];
  // deno-lint-ignore no-explicit-any
  liveSessionsError?: any;
}): Deps {
  const liveSessionIds = opts.liveSessionIds ?? [];
  return {
    adminClient: () =>
      ({
        from(table: string) {
          if (table === "sessions") {
            return {
              select: () => ({
                // One chained object serving two different real call
                // shapes against the same table: the sweep's own
                // top-level `.eq("status","live").limit(N)` query, and
                // summarizeSession's own per-session
                // `.eq("id", id).maybeSingle()` lookup.
                eq: () => ({
                  limit: () =>
                    Promise.resolve(
                      opts.liveSessionsError
                        ? { data: null, error: opts.liveSessionsError }
                        : {
                          data: liveSessionIds.map((id) => ({ id })),
                          error: null,
                        },
                    ),
                  maybeSingle: () =>
                    Promise.resolve({
                      data: {
                        id: "s",
                        project_id: "p1",
                        status: "live",
                        title: null,
                        title_source: "ai",
                        summary: null,
                        summarized_through_seq: null,
                        files_touched: [],
                      },
                      error: null,
                    }),
                }),
              }),
              update: () => ({ eq: () => Promise.resolve({ error: null }) }),
            };
          }
          if (table === "projects") {
            return {
              select: () => ({
                eq: () => ({
                  maybeSingle: () =>
                    Promise.resolve({
                      data: { bucket: null, daily_token_budget: 200_000 },
                      error: null,
                    }),
                }),
              }),
            };
          }
          if (table === "ai_usage") {
            return {
              select: () => ({
                eq: () => ({
                  gte: () => Promise.resolve({ data: [], error: null }),
                }),
              }),
              insert: () => Promise.resolve({ error: null }),
            };
          }
          if (table === "session_chunks") {
            return {
              select: () => ({
                eq: () => ({
                  eq: () => ({
                    order: () => Promise.resolve({ data: [], error: null }),
                  }),
                }),
              }),
            };
          }
          throw new Error(`unexpected table ${table}`);
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
    callAnthropic: (() =>
      Promise.resolve({
        text: "",
        usage: { inputTokens: 0, outputTokens: 0 },
      })) as Deps["callAnthropic"],
    sleep: () => Promise.resolve(),
    now: () => new Date(),
  };
}

function req(body: unknown = {}, cronSecret = "test-cron-secret"): Request {
  return new Request("http://local/functions/v1/summarize-session", {
    method: "POST",
    headers: {
      "content-type": "application/json",
      "x-cron-secret": cronSecret,
    },
    body: JSON.stringify(body),
  });
}

function withEnv(
  vars: Record<string, string | undefined>,
  fn: () => Promise<void>,
) {
  const prev: Record<string, string | undefined> = {};
  for (const [k, v] of Object.entries(vars)) {
    prev[k] = Deno.env.get(k);
    if (v === undefined) Deno.env.delete(k);
    else Deno.env.set(k, v);
  }
  return fn().finally(() => {
    for (const [k, v] of Object.entries(prev)) {
      if (v === undefined) Deno.env.delete(k);
      else Deno.env.set(k, v);
    }
  });
}

Deno.test("rejects non-POST", async () => {
  const res = await handle(new Request("http://local", { method: "GET" }));
  assertEquals(res.status, 405);
});

Deno.test("rejects a missing or wrong x-cron-secret", async () => {
  await withEnv({
    SUMMARIZE_CRON_SECRET: "real-secret",
    ANTHROPIC_API_KEY: "k",
  }, async () => {
    const res = await handle(req({}, "wrong-secret"), fakeDeps({}));
    assertEquals(res.status, 401);
  });
});

Deno.test("500s cleanly if ANTHROPIC_API_KEY isn't configured", async () => {
  await withEnv({
    SUMMARIZE_CRON_SECRET: "test-cron-secret",
    ANTHROPIC_API_KEY: undefined,
  }, async () => {
    const res = await handle(req(), fakeDeps({}));
    assertEquals(res.status, 500);
  });
});

Deno.test("sweeps every live session and reports counts, with no sessions to process", async () => {
  await withEnv({
    SUMMARIZE_CRON_SECRET: "test-cron-secret",
    ANTHROPIC_API_KEY: "k",
  }, async () => {
    const res = await handle(req(), fakeDeps({ liveSessionIds: [] }));
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body, { summarized: 0, skipped: 0, gave_up: 0 });
  });
});

Deno.test("sweeps multiple live sessions, each with no new chunks, and counts them all as skipped", async () => {
  await withEnv({
    SUMMARIZE_CRON_SECRET: "test-cron-secret",
    ANTHROPIC_API_KEY: "k",
  }, async () => {
    const res = await handle(
      req(),
      fakeDeps({ liveSessionIds: ["s1", "s2", "s3"] }),
    );
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body, { summarized: 0, skipped: 3, gave_up: 0 });
  });
});

Deno.test("one session's unexpected throw doesn't abort the sweep over the others", async () => {
  await withEnv({
    SUMMARIZE_CRON_SECRET: "test-cron-secret",
    ANTHROPIC_API_KEY: "k",
  }, async () => {
    let call = 0;
    const deps = fakeDeps({ liveSessionIds: ["s1", "s2"] });
    const originalAdminClient = deps.adminClient;
    deps.adminClient = () => {
      const client = originalAdminClient();
      const originalFrom = client.from.bind(client);
      // deno-lint-ignore no-explicit-any
      client.from = (table: string): any => {
        if (table === "sessions") {
          call++;
          if (call === 2) {
            // the lookup inside summarizeSession for the *second*
            // live session throws — the sweep must still finish and
            // report the first session's own real outcome.
            return {
              select: () => ({
                eq: () => ({
                  limit: () =>
                    Promise.resolve({
                      data: [{ id: "s1" }, { id: "s2" }],
                      error: null,
                    }),
                  maybeSingle: () => {
                    throw new Error("boom");
                  },
                }),
              }),
            };
          }
        }
        return originalFrom(table);
      };
      return client;
    };

    const res = await handle(req(), deps);
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body.summarized + body.skipped + body.gave_up, 2);
    assertEquals(body.gave_up >= 1, true);
  });
});

Deno.test("surfaces a live-session lookup failure as 500", async () => {
  await withEnv({
    SUMMARIZE_CRON_SECRET: "test-cron-secret",
    ANTHROPIC_API_KEY: "k",
  }, async () => {
    const res = await handle(
      req(),
      fakeDeps({ liveSessionsError: { code: "XXXXX", message: "boom" } }),
    );
    assertEquals(res.status, 500);
  });
});
