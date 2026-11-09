import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

// deno-lint-ignore no-explicit-any
function fakeUser(id = "caller-1"): any {
  return { id, email: "ali@devminified.com" };
}

type Row = {
  id: string;
  user_id: string;
  title: string | null;
  started_at: string;
  parent_session_id: string | null;
  project_id?: string;
};

// A small in-memory "sessions" table the fake admin/caller clients
// query against, so findRoot/walkDown's real iterative logic runs
// against real-shaped data instead of a hand-scripted sequence of
// mock responses (that pattern doesn't scale to a depth-10 chain).
function fakeDeps(opts: {
  user?: unknown;
  rows: Row[];
  memberRole?: string | null;
  hiddenIds?: Set<string>;
  memberError?: unknown;
}): Deps {
  const projectId = "p1";
  const byId = new Map(opts.rows.map((r) => [r.id, r]));
  const hidden = opts.hiddenIds ?? new Set<string>();

  // deno-lint-ignore no-explicit-any
  const adminClient = (): any => ({
    from(table: string) {
      if (table !== "sessions") throw new Error(`unexpected table ${table}`);
      return {
        select(_cols: string) {
          return {
            eq(_col: string, val: string) {
              return {
                maybeSingle: () => {
                  const row = byId.get(val);
                  return Promise.resolve({
                    data: row ? { ...row, project_id: projectId } : null,
                    error: null,
                  });
                },
              };
            },
            in(col: string, vals: string[]) {
              if (col !== "parent_session_id") {
                throw new Error(`unexpected in() column ${col}`);
              }
              const matches = opts.rows.filter(
                (r) =>
                  r.parent_session_id && vals.includes(r.parent_session_id),
              );
              return Promise.resolve({ data: matches, error: null });
            },
          };
        },
      };
    },
    auth: {
      admin: {
        getUserById: (uid: string) =>
          Promise.resolve({
            data: { user: { user_metadata: { user_name: `login-${uid}` } } },
            error: null,
          }),
      },
    },
  });

  // deno-lint-ignore no-explicit-any
  const callerClient = (): any => ({
    from(table: string) {
      if (table === "project_members") {
        return {
          select: () => ({
            eq: () => ({
              eq: () => ({
                maybeSingle: () => {
                  if (opts.memberError) {
                    return Promise.resolve({
                      data: null,
                      error: opts.memberError,
                    });
                  }
                  const role = opts.memberRole === undefined
                    ? "viewer"
                    : opts.memberRole;
                  return Promise.resolve({
                    data: role === null ? null : { role },
                    error: null,
                  });
                },
              }),
            }),
          }),
        };
      }
      if (table === "sessions") {
        return {
          select: () => ({
            in: (_col: string, ids: string[]) =>
              Promise.resolve({
                data: ids.filter((id) => !hidden.has(id)).map((id) => ({ id })),
                error: null,
              }),
          }),
        };
      }
      throw new Error(`unexpected table ${table}`);
    },
  });

  return {
    requireUser: () => Promise.resolve(opts.user as never),
    adminClient,
    callerClient,
  };
}

function req(body: unknown, auth = "Bearer token"): Request {
  return new Request("http://local/functions/v1/get-session-family", {
    method: "POST",
    headers: { Authorization: auth, "content-type": "application/json" },
    body: JSON.stringify(body),
  });
}

const ROOT: Row = {
  id: "11111111-1111-4111-8111-111111111111",
  user_id: "u1",
  title: "root session",
  started_at: "2026-01-01T00:00:00Z",
  parent_session_id: null,
};
const CHILD_A: Row = {
  id: "22222222-2222-4222-8222-222222222222",
  user_id: "u1",
  title: "fork a",
  started_at: "2026-01-02T00:00:00Z",
  parent_session_id: ROOT.id,
};
const CHILD_B: Row = {
  id: "33333333-3333-4333-8333-333333333333",
  user_id: "u2",
  title: "fork b",
  started_at: "2026-01-03T00:00:00Z",
  parent_session_id: ROOT.id,
};
const GRANDCHILD: Row = {
  id: "44444444-4444-4444-8444-444444444444",
  user_id: "u2",
  title: "fork of fork a",
  started_at: "2026-01-04T00:00:00Z",
  parent_session_id: CHILD_A.id,
};

Deno.test("rejects non-POST", async () => {
  const res = await handle(
    new Request("http://local", { method: "GET" }),
    fakeDeps({ user: fakeUser(), rows: [ROOT] }),
  );
  assertEquals(res.status, 405);
});

Deno.test("rejects a missing/invalid caller token", async () => {
  const res = await handle(
    req({ sessionId: ROOT.id }),
    fakeDeps({ user: null, rows: [ROOT] }),
  );
  assertEquals(res.status, 401);
});

Deno.test("returns 404 when the session doesn't exist", async () => {
  const res = await handle(
    req({ sessionId: ROOT.id }),
    fakeDeps({ user: fakeUser(), rows: [] }),
  );
  assertEquals(res.status, 404);
});

Deno.test("a non-member gets 403", async () => {
  const res = await handle(
    req({ sessionId: ROOT.id }),
    fakeDeps({ user: fakeUser(), rows: [ROOT], memberRole: null }),
  );
  assertEquals(res.status, 403);
});

Deno.test("a viewer gets 200 — the minimum role", async () => {
  const res = await handle(
    req({ sessionId: ROOT.id }),
    fakeDeps({ user: fakeUser(), rows: [ROOT], memberRole: "viewer" }),
  );
  assertEquals(res.status, 200);
});

Deno.test("builds the full tree (root, two children, one grandchild) from any node", async () => {
  const rows = [ROOT, CHILD_A, CHILD_B, GRANDCHILD];
  // asking from the grandchild must still discover the whole family,
  // not just its own ancestor chain
  const res = await handle(
    req({ sessionId: GRANDCHILD.id }),
    fakeDeps({ user: fakeUser(), rows }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  const ids = body.nodes.map((n: { id: string }) => n.id).sort();
  assertEquals(ids, [ROOT.id, CHILD_A.id, CHILD_B.id, GRANDCHILD.id].sort());

  const current = body.nodes.find((n: { id: string }) =>
    n.id === GRANDCHILD.id
  );
  assertEquals(current.isCurrent, true);
  assertEquals(current.parentId, CHILD_A.id);

  const siblings = body.nodes.filter((n: { parentId: string }) =>
    n.parentId === ROOT.id
  );
  assertEquals(siblings.length, 2); // CHILD_A and CHILD_B are siblings
});

Deno.test("a depth-10 chain resolves correctly from the deepest node", async () => {
  const rows: Row[] = [];
  for (let i = 0; i < 10; i++) {
    rows.push({
      id: `aaaaaaaa-aaaa-4aaa-8aaa-${String(i).padStart(12, "0")}`,
      user_id: "u1",
      title: `depth ${i}`,
      started_at: "2026-01-01T00:00:00Z",
      parent_session_id: i === 0 ? null : rows[i - 1].id,
    });
  }
  const deepest = rows[rows.length - 1];
  const res = await handle(
    req({ sessionId: deepest.id }),
    fakeDeps({ user: fakeUser(), rows }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.nodes.length, 10);
  assertEquals(body.danglingParentId, null);
});

Deno.test("a deleted parent is reported as a dangling reference, not an error", async () => {
  const dangling = "99999999-9999-4999-8999-999999999999";
  const orphan: Row = { ...CHILD_A, parent_session_id: dangling };
  const res = await handle(
    req({ sessionId: orphan.id }),
    fakeDeps({ user: fakeUser(), rows: [orphan] }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  assertEquals(body.danglingParentId, dangling);
  assertEquals(body.nodes.map((n: { id: string }) => n.id), [orphan.id]);
});

Deno.test("a fork the caller's RLS can't see comes back minimal, without details", async () => {
  const rows = [ROOT, CHILD_A, CHILD_B];
  const res = await handle(
    req({ sessionId: ROOT.id }),
    fakeDeps({ user: fakeUser(), rows, hiddenIds: new Set([CHILD_B.id]) }),
  );
  assertEquals(res.status, 200);
  const body = await res.json();
  const hiddenNode = body.nodes.find((n: { id: string }) =>
    n.id === CHILD_B.id
  );
  assertEquals(hiddenNode.accessible, false);
  assertEquals(hiddenNode.title, undefined);
  assertEquals(hiddenNode.ownerLogin, undefined);

  const visibleNode = body.nodes.find((n: { id: string }) =>
    n.id === CHILD_A.id
  );
  assertEquals(visibleNode.accessible, true);
  assertEquals(visibleNode.title, "fork a");
});

Deno.test("surfaces a membership lookup failure as 500", async () => {
  const res = await handle(
    req({ sessionId: ROOT.id }),
    fakeDeps({
      user: fakeUser(),
      rows: [ROOT],
      memberError: { code: "XXXXX", message: "boom" },
    }),
  );
  assertEquals(res.status, 500);
});
