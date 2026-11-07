import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

// deno-lint-ignore no-explicit-any
function fakeUser(id = "u1"): any {
  return { id, email: "ali@devminified.com" };
}

function fakeDeps(opts: {
  user?: unknown;
  // deno-lint-ignore no-explicit-any
  member?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  sessionUpsert?: { error?: any };
  // deno-lint-ignore no-explicit-any
  signedUrl?: { data?: any; error?: any };
  // deno-lint-ignore no-explicit-any
  onSessionUpsert?: (row: any) => void;
}): Deps {
  return {
    requireUser: () => Promise.resolve(opts.user as never),
    adminClient: () =>
      ({
        from(table: string) {
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
          if (table === "sessions") {
            return {
              // deno-lint-ignore no-explicit-any
              upsert: (row: any) => {
                opts.onSessionUpsert?.(row);
                return Promise.resolve(opts.sessionUpsert ?? { error: null });
              },
            };
          }
          throw new Error(`unexpected table ${table}`);
        },
        storage: {
          from(bucket: string) {
            if (bucket !== "carryon-sessions") {
              throw new Error(`unexpected bucket ${bucket}`);
            }
            return {
              createSignedUploadUrl: () =>
                Promise.resolve(
                  opts.signedUrl ?? {
                    data: {
                      signedUrl: "http://local/signed",
                      token: "tok",
                      path: "p",
                    },
                    error: null,
                  },
                ),
            };
          },
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
  };
}

function validBody(overrides: Record<string, unknown> = {}) {
  return {
    sessionId: "11111111-1111-4111-8111-111111111111",
    projectId: "22222222-2222-4222-8222-222222222222",
    originalCwd: "/repo",
    streamId: "main",
    seq: 0,
    byteStart: 0,
    byteEnd: 100,
    sha256: "a".repeat(64),
    size: 100,
    ...overrides,
  };
}

function req(body: unknown, auth = "Bearer token"): Request {
  return new Request("http://local/functions/v1/register-chunk", {
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
  const res = await handle(req(validBody()), fakeDeps({ user: null }));
  assertEquals(res.status, 401);
});

Deno.test("rejects byteEnd <= byteStart", async () => {
  const res = await handle(
    req(validBody({ byteStart: 100, byteEnd: 100 })),
    fakeDeps({ user: fakeUser() }),
  );
  assertEquals(res.status, 400);
});

Deno.test("accepts size independent of byteEnd - byteStart (CO-301: redaction changes length)", async () => {
  // byteStart/byteEnd describe the original file's range; size is the
  // actual uploaded (possibly redacted) object's length — these are no
  // longer required to match once anything was redacted.
  const res = await handle(
    req(validBody({ size: 999 })),
    fakeDeps({
      user: fakeUser(),
      member: { data: { role: "resumer" }, error: null },
    }),
  );
  assertEquals(res.status, 200);
});

Deno.test("rejects a caller who isn't a project member", async () => {
  const res = await handle(
    req(validBody()),
    fakeDeps({ user: fakeUser(), member: { data: null, error: null } }),
  );
  assertEquals(res.status, 403);
});

Deno.test("upserts the session and returns a signed upload token", async () => {
  const deps = fakeDeps({
    user: fakeUser("u1"),
    member: { data: { role: "resumer" }, error: null },
    sessionUpsert: { error: null },
    signedUrl: {
      data: {
        signedUrl: "http://kong:8000/signed?token=abc",
        token: "abc",
        path: "x",
      },
      error: null,
    },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 200);
  const body = await res.json();
  // Only the token is returned, never signedUrl — see index.ts's comment
  // on why (it bakes in this function's own internal SUPABASE_URL).
  assertEquals(body.uploadToken, "abc");
  assertEquals(body.uploadUrl, undefined);
  assertEquals(
    body.objectKey,
    "22222222-2222-4222-8222-222222222222/u1/11111111-1111-4111-8111-111111111111/main/chunk-0.jsonl.zst",
  );
});

Deno.test("passes machine/branch/headSha/claudeCodeVersion through to the session upsert", async () => {
  // deno-lint-ignore no-explicit-any
  let upserted: any;
  const deps = fakeDeps({
    user: fakeUser("u1"),
    member: { data: { role: "resumer" }, error: null },
    onSessionUpsert: (row) => {
      upserted = row;
    },
  });

  const res = await handle(
    req(validBody({
      machine: "alis-macbook",
      branch: "main",
      headSha: "a".repeat(40),
      claudeCodeVersion: "1.2.3",
    })),
    deps,
  );
  assertEquals(res.status, 200);
  assertEquals(upserted.machine, "alis-macbook");
  assertEquals(upserted.branch, "main");
  assertEquals(upserted.head_sha, "a".repeat(40));
  assertEquals(upserted.claude_code_version, "1.2.3");
});

Deno.test("CO-111 fields are optional — their absence doesn't fail registration", async () => {
  const deps = fakeDeps({
    user: fakeUser("u1"),
    member: { data: { role: "resumer" }, error: null },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 200);
});

Deno.test("surfaces a session upsert failure as 500", async () => {
  const deps = fakeDeps({
    user: fakeUser(),
    member: { data: { role: "resumer" }, error: null },
    sessionUpsert: { error: { code: "XXXXX", message: "boom" } },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 500);
});

Deno.test("surfaces a signing failure as 500", async () => {
  const deps = fakeDeps({
    user: fakeUser(),
    member: { data: { role: "resumer" }, error: null },
    sessionUpsert: { error: null },
    signedUrl: { data: null, error: { message: "boom" } },
  });

  const res = await handle(req(validBody()), deps);
  assertEquals(res.status, 500);
});
