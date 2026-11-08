import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

const CRON_SECRET = "test-cron-secret";
Deno.env.set("PURGE_CRON_SECRET", CRON_SECRET);

interface ProjectRow {
  id: string;
  bucket: string | null;
  closed_at: string | null;
  retention_days_after_close: number;
}
interface SessionRow {
  id: string;
  project_id: string;
}
interface ChunkRow {
  session_id: string;
  object_key: string;
}
interface SnapshotRow {
  session_id: string;
  object_key: string | null;
}

interface FakeState {
  projects: ProjectRow[];
  sessions: SessionRow[];
  chunks: ChunkRow[];
  snapshots: SnapshotRow[];
  // deno-lint-ignore no-explicit-any
  auditLog: any[];
  storageObjects: Set<string>; // "bucket/key"
  removeErr?: unknown;
  sessionLookupErrForProject?: string;
}

function freshState(overrides: Partial<FakeState> = {}): FakeState {
  return {
    projects: [],
    sessions: [],
    chunks: [],
    snapshots: [],
    auditLog: [],
    storageObjects: new Set(),
    ...overrides,
  };
}

// deno-lint-ignore no-explicit-any
function fakeAdminClient(state: FakeState): any {
  return {
    from(table: string) {
      if (table === "projects") {
        return {
          select: () => ({
            not: () =>
              Promise.resolve({
                data: state.projects.filter((p) => p.closed_at !== null),
                error: null,
              }),
          }),
        };
      }
      if (table === "sessions") {
        return {
          select: () => ({
            eq: (_c: string, projectID: string) => {
              if (projectID === state.sessionLookupErrForProject) {
                return Promise.resolve({ data: null, error: { code: "boom" } });
              }
              return Promise.resolve({
                data: state.sessions.filter((s) => s.project_id === projectID),
                error: null,
              });
            },
          }),
          delete: () => ({
            in: (_c: string, ids: string[]) => {
              state.sessions = state.sessions.filter((s) =>
                !ids.includes(s.id)
              );
              state.chunks = state.chunks.filter((c) =>
                !ids.includes(c.session_id)
              );
              state.snapshots = state.snapshots.filter((s) =>
                !ids.includes(s.session_id)
              );
              return Promise.resolve({ error: null });
            },
          }),
        };
      }
      if (table === "session_chunks") {
        return {
          select: () => ({
            in: (_c: string, ids: string[]) =>
              Promise.resolve({
                data: state.chunks.filter((c) => ids.includes(c.session_id)),
                error: null,
              }),
          }),
        };
      }
      if (table === "code_snapshots") {
        return {
          select: () => ({
            in: (_c: string, ids: string[]) => ({
              not: () =>
                Promise.resolve({
                  data: state.snapshots.filter((s) =>
                    ids.includes(s.session_id) && s.object_key !== null
                  ),
                  error: null,
                }),
            }),
          }),
        };
      }
      if (table === "audit_log") {
        return {
          delete: () => ({
            eq: (_c: string, projectID: string) => {
              state.auditLog = state.auditLog.filter((a) =>
                a.project_id !== projectID
              );
              return Promise.resolve({ error: null });
            },
          }),
        };
      }
      throw new Error(`unexpected table ${table}`);
    },
    storage: {
      from(bucket: string) {
        return {
          remove: (keys: string[]) => {
            if (state.removeErr) {
              return Promise.resolve({ error: state.removeErr });
            }
            for (const k of keys) state.storageObjects.delete(`${bucket}/${k}`);
            return Promise.resolve({ error: null });
          },
        };
      },
    },
  };
}

function fakeDeps(state: FakeState): Deps {
  return { adminClient: () => fakeAdminClient(state) };
}

function req(secret = CRON_SECRET): Request {
  return new Request("http://localhost/purge-expired-sessions", {
    method: "POST",
    headers: { "x-cron-secret": secret, "content-type": "application/json" },
    body: "{}",
  });
}

function daysAgo(n: number): string {
  return new Date(Date.now() - n * 24 * 60 * 60 * 1000).toISOString();
}

Deno.test("rejects non-POST", async () => {
  const res = await handle(
    new Request("http://localhost/x", { method: "GET" }),
  );
  assertEquals(res.status, 405);
});

Deno.test("rejects a missing or wrong cron secret", async () => {
  const res = await handle(req("wrong"), fakeDeps(freshState()));
  assertEquals(res.status, 401);
});

Deno.test("an active project (closed_at null) is never touched", async () => {
  const state = freshState({
    projects: [{
      id: "p1",
      bucket: null,
      closed_at: null,
      retention_days_after_close: 90,
    }],
    sessions: [{ id: "s1", project_id: "p1" }],
    chunks: [{
      session_id: "s1",
      object_key: "p1/u1/s1/main/chunk-0.jsonl.zst",
    }],
  });
  state.storageObjects.add("carryon-sessions/p1/u1/s1/main/chunk-0.jsonl.zst");

  const res = await handle(req(), fakeDeps(state));
  const body = await res.json();
  assertEquals(body.projectsProcessed, 0);
  assertEquals(body.sessionsDeleted, 0);
  assertEquals(state.sessions.length, 1);
  assertEquals(state.storageObjects.size, 1);
});

Deno.test("a closed project still within its retention window is untouched", async () => {
  const state = freshState({
    projects: [{
      id: "p1",
      bucket: null,
      closed_at: daysAgo(10),
      retention_days_after_close: 90,
    }],
    sessions: [{ id: "s1", project_id: "p1" }],
  });

  const res = await handle(req(), fakeDeps(state));
  const body = await res.json();
  assertEquals(body.projectsProcessed, 0);
  assertEquals(body.sessionsDeleted, 0);
  assertEquals(state.sessions.length, 1);
});

Deno.test("an expired project is fully purged: sessions, chunks, snapshots, storage objects, audit rows", async () => {
  const state = freshState({
    projects: [{
      id: "p1",
      bucket: null,
      closed_at: daysAgo(100),
      retention_days_after_close: 90,
    }],
    sessions: [{ id: "s1", project_id: "p1" }],
    chunks: [{
      session_id: "s1",
      object_key: "p1/u1/s1/main/chunk-0.jsonl.zst",
    }],
    snapshots: [{ session_id: "s1", object_key: "p1/u1/s1/code-0.bundle" }],
    auditLog: [{ project_id: "p1", action: "upload", session_id: "s1" }],
  });
  state.storageObjects.add("carryon-sessions/p1/u1/s1/main/chunk-0.jsonl.zst");
  state.storageObjects.add("carryon-sessions/p1/u1/s1/code-0.bundle");

  const res = await handle(req(), fakeDeps(state));
  const body = await res.json();
  assertEquals(body.dryRun, false);
  assertEquals(body.projectsProcessed, 1);
  assertEquals(body.sessionsDeleted, 1);
  assertEquals(body.report, [{
    projectId: "p1",
    sessionsDeleted: 1,
    objectsRemoved: 2,
  }]);
  assertEquals(state.sessions.length, 0);
  assertEquals(state.chunks.length, 0);
  assertEquals(state.snapshots.length, 0);
  assertEquals(state.storageObjects.size, 0);
  assertEquals(state.auditLog.length, 0);
});

Deno.test("CO-305: purges objects from a project's dedicated bucket, not the shared default", async () => {
  const state = freshState({
    projects: [{
      id: "p1",
      bucket: "client-x-bucket",
      closed_at: daysAgo(100),
      retention_days_after_close: 90,
    }],
    sessions: [{ id: "s1", project_id: "p1" }],
    chunks: [{
      session_id: "s1",
      object_key: "p1/u1/s1/main/chunk-0.jsonl.zst",
    }],
  });
  state.storageObjects.add("client-x-bucket/p1/u1/s1/main/chunk-0.jsonl.zst");

  const res = await handle(req(), fakeDeps(state));
  const body = await res.json();
  assertEquals(body.projectsProcessed, 1);
  assertEquals(body.sessionsDeleted, 1);
  assertEquals(state.storageObjects.size, 0);
});

Deno.test("a storage removal failure skips that project's DB deletes entirely", async () => {
  const state = freshState({
    projects: [{
      id: "p1",
      bucket: null,
      closed_at: daysAgo(100),
      retention_days_after_close: 90,
    }],
    sessions: [{ id: "s1", project_id: "p1" }],
    chunks: [{
      session_id: "s1",
      object_key: "p1/u1/s1/main/chunk-0.jsonl.zst",
    }],
    removeErr: { message: "boom" },
  });

  const res = await handle(req(), fakeDeps(state));
  const body = await res.json();
  assertEquals(body.projectsProcessed, 0);
  assertEquals(body.sessionsDeleted, 0);
  assertEquals(state.sessions.length, 1); // never deleted — bytes might still be there
});

Deno.test("one expired project with no sessions left still counts as processed", async () => {
  const state = freshState({
    projects: [{
      id: "p1",
      bucket: null,
      closed_at: daysAgo(100),
      retention_days_after_close: 90,
    }],
  });
  const res = await handle(req(), fakeDeps(state));
  const body = await res.json();
  assertEquals(body.projectsProcessed, 1);
  assertEquals(body.sessionsDeleted, 0);
});

Deno.test("dryRun: true reports what would be deleted without deleting anything", async () => {
  const state = freshState({
    projects: [{
      id: "p1",
      bucket: null,
      closed_at: daysAgo(100),
      retention_days_after_close: 90,
    }],
    sessions: [{ id: "s1", project_id: "p1" }],
    chunks: [{
      session_id: "s1",
      object_key: "p1/u1/s1/main/chunk-0.jsonl.zst",
    }],
    auditLog: [{ project_id: "p1", action: "upload", session_id: "s1" }],
  });
  state.storageObjects.add("carryon-sessions/p1/u1/s1/main/chunk-0.jsonl.zst");

  const res = await handle(
    new Request("http://localhost/purge-expired-sessions", {
      method: "POST",
      headers: {
        "x-cron-secret": CRON_SECRET,
        "content-type": "application/json",
      },
      body: JSON.stringify({ dryRun: true }),
    }),
    fakeDeps(state),
  );
  const body = await res.json();
  assertEquals(body.dryRun, true);
  assertEquals(body.projectsProcessed, 1);
  assertEquals(body.sessionsDeleted, 1);
  assertEquals(body.report, [{
    projectId: "p1",
    sessionsDeleted: 1,
    objectsRemoved: 1,
  }]);

  // nothing was actually touched
  assertEquals(state.sessions.length, 1);
  assertEquals(state.chunks.length, 1);
  assertEquals(state.storageObjects.size, 1);
  assertEquals(state.auditLog.length, 1);
});

Deno.test("rejects a non-boolean dryRun", async () => {
  const res = await handle(
    new Request("http://localhost/purge-expired-sessions", {
      method: "POST",
      headers: {
        "x-cron-secret": CRON_SECRET,
        "content-type": "application/json",
      },
      body: JSON.stringify({ dryRun: "yes" }),
    }),
    fakeDeps(freshState()),
  );
  assertEquals(res.status, 400);
});

Deno.test("two expired projects: one fails, the other still gets purged", async () => {
  const state = freshState({
    projects: [
      {
        id: "p-bad",
        bucket: null,
        closed_at: daysAgo(100),
        retention_days_after_close: 90,
      },
      {
        id: "p-good",
        bucket: null,
        closed_at: daysAgo(100),
        retention_days_after_close: 90,
      },
    ],
    sessions: [
      { id: "s-bad", project_id: "p-bad" },
      { id: "s-good", project_id: "p-good" },
    ],
    chunks: [
      { session_id: "s-bad", object_key: "bad-key" },
      { session_id: "s-good", object_key: "good-key" },
    ],
    sessionLookupErrForProject: "p-bad",
  });
  state.storageObjects.add("carryon-sessions/good-key");

  const res = await handle(req(), fakeDeps(state));
  const body = await res.json();
  assertEquals(body.projectsProcessed, 1);
  assertEquals(body.sessionsDeleted, 1);
  assertEquals(state.sessions.some((s) => s.id === "s-good"), false);
  assertEquals(state.sessions.some((s) => s.id === "s-bad"), true); // untouched — its project failed
});
