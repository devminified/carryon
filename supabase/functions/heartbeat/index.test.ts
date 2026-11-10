import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

// deno-lint-ignore no-explicit-any
function fakeUser(id = "u1"): any {
  return { id, email: "ali@devminified.com" };
}

function fakeDeps(opts: {
  user?: unknown;
  // deno-lint-ignore no-explicit-any
  upsertError?: any;
  onUpsert?: (row: Record<string, unknown>) => void;
}): Deps {
  return {
    requireUser: () => Promise.resolve(opts.user as never),
    callerClient: () =>
      ({
        from(table: string) {
          if (table === "agent_heartbeats") {
            return {
              upsert: (row: Record<string, unknown>) => {
                opts.onUpsert?.(row);
                return Promise.resolve({
                  error: opts.upsertError ?? null,
                });
              },
            };
          }
          throw new Error(`unexpected table ${table}`);
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
  };
}

function req(body: unknown): Request {
  return new Request("http://localhost/heartbeat", {
    method: "POST",
    headers: {
      Authorization: "Bearer tok",
      "content-type": "application/json",
    },
    body: JSON.stringify(body),
  });
}

const VALID_BODY = {
  machineId: "machine-1",
  hostname: "alis-laptop",
  version: "1.2.3",
  blockedSessions: 0,
};

Deno.test("rejects non-POST", async () => {
  const res = await handle(
    new Request("http://localhost/x", { method: "GET" }),
  );
  assertEquals(res.status, 405);
});

Deno.test("rejects a missing or invalid caller token", async () => {
  const res = await handle(req(VALID_BODY), fakeDeps({ user: null }));
  assertEquals(res.status, 401);
});

Deno.test("rejects an invalid body", async () => {
  const res = await handle(
    req({ machineId: "", hostname: "h", version: "1.0", blockedSessions: 0 }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});

Deno.test("rejects a negative blockedSessions", async () => {
  const res = await handle(
    req({ ...VALID_BODY, blockedSessions: -1 }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});

Deno.test("upserts the caller's own row, never a client-supplied user_id", async () => {
  // deno-lint-ignore no-explicit-any
  let upserted: any = null;
  const res = await handle(
    req({ ...VALID_BODY, userId: "someone-else" }), // extra field, ignored by the schema
    fakeDeps({
      user: fakeUser("real-caller"),
      onUpsert: (row) => {
        upserted = row;
      },
    }),
  );
  assertEquals(res.status, 200);
  assertEquals(upserted?.user_id, "real-caller");
  assertEquals(upserted?.machine_id, "machine-1");
  assertEquals(upserted?.hostname, "alis-laptop");
  assertEquals(upserted?.version, "1.2.3");
  assertEquals(upserted?.blocked_sessions, 0);
});

Deno.test("surfaces an upsert failure as 500", async () => {
  const res = await handle(
    req(VALID_BODY),
    fakeDeps({ user: fakeUser(), upsertError: { code: "XXXXX" } }),
  );
  assertEquals(res.status, 500);
});

// CO-504
Deno.test("an older agent binary with none of the new fields still upserts, with honest defaults", async () => {
  // deno-lint-ignore no-explicit-any
  let upserted: any = null;
  const res = await handle(
    req(VALID_BODY),
    fakeDeps({ user: fakeUser(), onUpsert: (row) => (upserted = row) }),
  );
  assertEquals(res.status, 200);
  assertEquals(upserted?.os, null);
  assertEquals(upserted?.bytes_behind, 0);
  assertEquals(upserted?.last_upload_at, null);
  assertEquals(upserted?.upload_attempts, 0);
  assertEquals(upserted?.upload_errors, 0);
  assertEquals(upserted?.redactions_applied, 0);
});

Deno.test("passes through every CO-504 field when the agent sends them", async () => {
  // deno-lint-ignore no-explicit-any
  let upserted: any = null;
  const res = await handle(
    req({
      ...VALID_BODY,
      os: "darwin",
      bytesBehind: 1536,
      lastUploadAt: "2026-11-10T12:00:00Z",
      uploadAttempts: 10,
      uploadErrors: 1,
      redactionsApplied: 3,
    }),
    fakeDeps({ user: fakeUser(), onUpsert: (row) => (upserted = row) }),
  );
  assertEquals(res.status, 200);
  assertEquals(upserted?.os, "darwin");
  assertEquals(upserted?.bytes_behind, 1536);
  assertEquals(upserted?.last_upload_at, "2026-11-10T12:00:00Z");
  assertEquals(upserted?.upload_attempts, 10);
  assertEquals(upserted?.upload_errors, 1);
  assertEquals(upserted?.redactions_applied, 3);
});

Deno.test("rejects a non-ISO-datetime lastUploadAt", async () => {
  const res = await handle(
    req({ ...VALID_BODY, lastUploadAt: "not-a-date" }),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});
