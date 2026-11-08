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
  project?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  chunkRows?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  snapshotRows?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  removeErr?: any;
  // deno-lint-ignore no-explicit-any
  callerDeleteRows?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  auditErr?: any;
  // deno-lint-ignore no-explicit-any
  onAuditInsert?: (row: any) => void;
  onRemove?: (bucket: string, keys: string[]) => void;
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
          if (table === "projects") {
            return {
              select: () => ({
                eq: () => ({
                  maybeSingle: () =>
                    Promise.resolve(
                      opts.project ?? { data: { bucket: null }, error: null },
                    ),
                }),
              }),
            };
          }
          if (table === "session_chunks") {
            return {
              select: () => ({
                eq: () =>
                  Promise.resolve(opts.chunkRows ?? { data: [], error: null }),
              }),
            };
          }
          if (table === "code_snapshots") {
            return {
              select: () => ({
                eq: () => ({
                  not: () =>
                    Promise.resolve(
                      opts.snapshotRows ?? { data: [], error: null },
                    ),
                }),
              }),
            };
          }
          if (table === "audit_log") {
            return {
              // deno-lint-ignore no-explicit-any
              insert: (row: any) => {
                opts.onAuditInsert?.(row);
                return Promise.resolve(
                  opts.auditErr ? { error: opts.auditErr } : { error: null },
                );
              },
            };
          }
          throw new Error(`unexpected admin table ${table}`);
        },
        storage: {
          from: (bucket: string) => ({
            remove: (keys: string[]) => {
              opts.onRemove?.(bucket, keys);
              return Promise.resolve(
                opts.removeErr ? { error: opts.removeErr } : { error: null },
              );
            },
          }),
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
    callerClient: () =>
      ({
        from(table: string) {
          if (table === "sessions") {
            return {
              delete: () => ({
                eq: () => ({
                  select: () =>
                    Promise.resolve(
                      opts.callerDeleteRows ??
                        { data: [{ id: "s1" }], error: null },
                    ),
                }),
              }),
            };
          }
          throw new Error(`unexpected caller table ${table}`);
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
  };
}

function req(body: unknown): Request {
  return new Request("http://localhost/delete-session", {
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
    req({ sessionId: SESSION_ID, reason: "cleanup" }),
    fakeDeps({ user: null }),
  );
  assertEquals(res.status, 401);
});

Deno.test("a deletion without a reason is refused", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});

Deno.test("a deletion with an empty or whitespace-only reason is refused", async () => {
  const res1 = await handle(
    req({ sessionId: SESSION_ID, reason: "" }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res1.status, 400);
  const res2 = await handle(
    req({ sessionId: SESSION_ID, reason: "   " }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res2.status, 400);
});

Deno.test("returns 404 when the session doesn't exist", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, reason: "cleanup" }),
    fakeDeps({ user: fakeUser(), session: { data: null, error: null } }),
  );
  assertEquals(res.status, 404);
});

Deno.test("a non-admin (resumer) is refused", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, reason: "cleanup" }),
    fakeDeps({
      user: fakeUser(),
      member: { data: { role: "resumer" }, error: null },
    }),
  );
  assertEquals(res.status, 403);
});

Deno.test("a non-member is refused", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, reason: "cleanup" }),
    fakeDeps({ user: fakeUser(), member: { data: null, error: null } }),
  );
  assertEquals(res.status, 403);
});

Deno.test("an admin succeeds: removes storage objects, deletes the row, audits the reason", async () => {
  let removedBucket = "";
  let removedKeys: string[] = [];
  // deno-lint-ignore no-explicit-any
  let auditRow: any = null;

  const res = await handle(
    req({ sessionId: SESSION_ID, reason: "developer left the team" }),
    fakeDeps({
      user: fakeUser(),
      chunkRows: {
        data: [{ object_key: "p1/u1/s1/main/chunk-0.jsonl.zst" }],
        error: null,
      },
      snapshotRows: {
        data: [{ object_key: "p1/u1/s1/code-0.bundle" }],
        error: null,
      },
      onRemove: (b, k) => {
        removedBucket = b;
        removedKeys = k;
      },
      onAuditInsert: (row) => {
        auditRow = row;
      },
    }),
  );
  assertEquals(res.status, 200);
  assertEquals(removedBucket, "carryon-sessions");
  assertEquals(
    removedKeys.sort(),
    ["p1/u1/s1/code-0.bundle", "p1/u1/s1/main/chunk-0.jsonl.zst"].sort(),
  );
  assertEquals(auditRow.action, "delete_session");
  assertEquals(auditRow.session_id, SESSION_ID);
  assertEquals(auditRow.project_id, "p1");
  assertEquals(auditRow.reason, "developer left the team");
});

Deno.test("CO-305: removes objects from a project's dedicated bucket", async () => {
  let removedBucket = "";
  await handle(
    req({ sessionId: SESSION_ID, reason: "cleanup" }),
    fakeDeps({
      user: fakeUser(),
      project: { data: { bucket: "client-x-bucket" }, error: null },
      chunkRows: { data: [{ object_key: "key1" }], error: null },
      onRemove: (b) => {
        removedBucket = b;
      },
    }),
  );
  assertEquals(removedBucket, "client-x-bucket");
});

Deno.test("if RLS denies the caller-client delete (0 rows), responds 403 and never audits", async () => {
  let audited = false;
  const res = await handle(
    req({ sessionId: SESSION_ID, reason: "cleanup" }),
    fakeDeps({
      user: fakeUser(),
      callerDeleteRows: { data: [], error: null },
      onAuditInsert: () => {
        audited = true;
      },
    }),
  );
  assertEquals(res.status, 403);
  assertEquals(audited, false);
});

Deno.test("a storage removal failure is surfaced as 500, and the session row is not deleted", async () => {
  let callerDeleteCalled = false;
  const deps = fakeDeps({
    user: fakeUser(),
    chunkRows: { data: [{ object_key: "key1" }], error: null },
    removeErr: { message: "boom" },
  });
  const wrapped: Deps = {
    ...deps,
    callerClient: () => {
      callerDeleteCalled = true;
      return deps.callerClient("tok");
    },
  };
  const res = await handle(
    req({ sessionId: SESSION_ID, reason: "cleanup" }),
    wrapped,
  );
  assertEquals(res.status, 500);
  assertEquals(callerDeleteCalled, false);
});

Deno.test("surfaces a membership lookup failure as 500", async () => {
  const res = await handle(
    req({ sessionId: SESSION_ID, reason: "cleanup" }),
    fakeDeps({
      user: fakeUser(),
      member: { data: null, error: { code: "XXXXX" } },
    }),
  );
  assertEquals(res.status, 500);
});
