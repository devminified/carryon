import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

// deno-lint-ignore no-explicit-any
function fakeUser(id = "u1"): any {
  return { id, email: "ali@devminified.com" };
}

function fakeDeps(opts: {
  user?: unknown;
  embedding?: number[];
  embedError?: Error;
  // deno-lint-ignore no-explicit-any
  rpcResult?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  sessionsResult?: { data?: any; error?: any };
  onRpcCall?: (fn: string, args: Record<string, unknown>) => void;
}): Deps {
  return {
    requireUser: () => Promise.resolve(opts.user as never),
    embedText: () =>
      opts.embedError
        ? Promise.reject(opts.embedError)
        : Promise.resolve(opts.embedding ?? [0.1, 0.2, 0.3]),
    callerClient: () =>
      ({
        rpc: (fn: string, args: Record<string, unknown>) => {
          opts.onRpcCall?.(fn, args);
          return Promise.resolve(
            opts.rpcResult ?? {
              data: [
                {
                  session_id: "s1",
                  score: 0.9,
                  matching_passage: "fixed the webhook retry",
                },
              ],
              error: null,
            },
          );
        },
        from(table: string) {
          if (table === "sessions") {
            return {
              select: () => ({
                in: () =>
                  Promise.resolve(
                    opts.sessionsResult ?? {
                      data: [
                        {
                          id: "s1",
                          project_id: "p1",
                          title: "fix webhook",
                          summary: "fixed the retry bug",
                          outcome: "done",
                          last_activity_at: "2026-06-15T12:00:00Z",
                        },
                      ],
                      error: null,
                    },
                  ),
              }),
            };
          }
          throw new Error(`unexpected table ${table}`);
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
  };
}

function req(body: unknown): Request {
  return new Request("http://localhost/search", {
    method: "POST",
    headers: {
      Authorization: "Bearer tok",
      "content-type": "application/json",
    },
    body: JSON.stringify(body),
  });
}

Deno.test("rejects non-POST", async () => {
  const res = await handle(
    new Request("http://localhost/x", { method: "GET" }),
  );
  assertEquals(res.status, 405);
});

Deno.test("rejects a missing or invalid caller token", async () => {
  const res = await handle(req({ query: "webhook" }), fakeDeps({ user: null }));
  assertEquals(res.status, 401);
});

Deno.test("rejects an empty query", async () => {
  const res = await handle(req({ query: "" }), fakeDeps({ user: fakeUser() }));
  assertEquals(res.status, 400);
});

Deno.test("rejects an invalid projectId", async () => {
  const res = await handle(
    req({ query: "webhook", projectId: "not-a-uuid" }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});

Deno.test("embeds the query and passes it, the project filter, and match count through to search_sessions", async () => {
  let calledFn = "";
  // deno-lint-ignore no-explicit-any
  let calledArgs: any = null;
  const res = await handle(
    req({
      query: "webhook retry",
      projectId: "11111111-1111-4111-8111-111111111111",
      matchCount: 5,
    }),
    fakeDeps({
      user: fakeUser(),
      embedding: [0.4, 0.5],
      onRpcCall: (fn, args) => {
        calledFn = fn;
        calledArgs = args;
      },
    }),
  );
  assertEquals(res.status, 200);
  assertEquals(calledFn, "search_sessions");
  assertEquals(calledArgs.p_query_embedding, [0.4, 0.5]);
  assertEquals(calledArgs.p_query_text, "webhook retry");
  assertEquals(calledArgs.p_project_id, "11111111-1111-4111-8111-111111111111");
  assertEquals(calledArgs.p_match_count, 5);
});

Deno.test("defaults matchCount to 20 and projectId to null when not supplied", async () => {
  // deno-lint-ignore no-explicit-any
  let calledArgs: any = null;
  await handle(
    req({ query: "webhook" }),
    fakeDeps({
      user: fakeUser(),
      onRpcCall: (_fn, args) => (calledArgs = args),
    }),
  );
  assertEquals(calledArgs.p_match_count, 20);
  assertEquals(calledArgs.p_project_id, null);
});

Deno.test("returns ranked results joined with each session's own title/summary/outcome", async () => {
  const res = await handle(
    req({ query: "webhook" }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.results.length, 1);
  assertEquals(body.results[0].sessionId, "s1");
  assertEquals(body.results[0].title, "fix webhook");
  assertEquals(body.results[0].score, 0.9);
  assertEquals(body.results[0].matchingPassage, "fixed the webhook retry");
});

Deno.test("an empty match list returns an empty result array without querying sessions", async () => {
  const res = await handle(
    req({ query: "something nobody ever did" }),
    fakeDeps({ user: fakeUser(), rpcResult: { data: [], error: null } }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.results, []);
});

Deno.test("a session search_sessions returned but the follow-up fetch can't see (RLS changed mid-request) is silently dropped, not errored", async () => {
  const res = await handle(
    req({ query: "webhook" }),
    fakeDeps({
      user: fakeUser(),
      sessionsResult: { data: [], error: null },
    }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.results, []);
});

Deno.test("surfaces an embedding failure as 500", async () => {
  const res = await handle(
    req({ query: "webhook" }),
    fakeDeps({ user: fakeUser(), embedError: new Error("model not loaded") }),
  );
  assertEquals(res.status, 500);
});

Deno.test("surfaces a search_sessions rpc failure as 500", async () => {
  const res = await handle(
    req({ query: "webhook" }),
    fakeDeps({
      user: fakeUser(),
      rpcResult: { data: null, error: { code: "XXXXX" } },
    }),
  );
  assertEquals(res.status, 500);
});

Deno.test("surfaces a sessions fetch failure as 500", async () => {
  const res = await handle(
    req({ query: "webhook" }),
    fakeDeps({
      user: fakeUser(),
      sessionsResult: { data: null, error: { code: "XXXXX" } },
    }),
  );
  assertEquals(res.status, 500);
});
