import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";
import type { GitHubTeamMember } from "../_shared/github.ts";

const CRON_SECRET = "test-cron-secret";

const PROJECT_WITH_TEAMS = {
  id: "p1",
  team_viewer: "100",
  team_resumer: "200",
  team_admin: "300",
};

// deno-lint-ignore no-explicit-any
type FakeAdminClient = any;

function fakeAdminClient(state: {
  projects: typeof PROJECT_WITH_TEAMS[];
  members: {
    project_id: string;
    user_id: string;
    role: string;
    source: string;
  }[];
  // deno-lint-ignore no-explicit-any
  auditLog: any[];
}): FakeAdminClient {
  return {
    from(table: string) {
      if (table === "projects") {
        return {
          select: () => ({
            or: () => Promise.resolve({ data: state.projects, error: null }),
          }),
        };
      }
      if (table === "project_members") {
        return {
          select: () => ({
            eq: (_col: string, projectID: string) =>
              Promise.resolve({
                data: state.members.filter((m) => m.project_id === projectID),
                error: null,
              }),
          }),
          // deno-lint-ignore no-explicit-any
          upsert: (row: any) => {
            const i = state.members.findIndex(
              (m) =>
                m.project_id === row.project_id && m.user_id === row.user_id,
            );
            if (i >= 0) state.members[i] = row;
            else state.members.push(row);
            return Promise.resolve({ error: null });
          },
          delete: () => ({
            eq: (_c1: string, projectID: string) => ({
              eq: (_c2: string, userID: string) => {
                state.members = state.members.filter(
                  (m) => !(m.project_id === projectID && m.user_id === userID),
                );
                return Promise.resolve({ error: null });
              },
            }),
          }),
        };
      }
      if (table === "audit_log") {
        return {
          // deno-lint-ignore no-explicit-any
          insert: (row: any) => {
            state.auditLog.push(row);
            return Promise.resolve({ error: null });
          },
        };
      }
      throw new Error(`unexpected table ${table}`);
    },
  };
}

type MemberRow = {
  project_id: string;
  user_id: string;
  role: string;
  source: string;
};
type FakeState = {
  projects: typeof PROJECT_WITH_TEAMS[];
  members: MemberRow[];
  // deno-lint-ignore no-explicit-any
  auditLog: any[];
};

function fakeDeps(opts: {
  projects?: typeof PROJECT_WITH_TEAMS[];
  members?: MemberRow[];
  githubUserIDMap?: Map<number, string>;
  teamMembers?: Record<string, GitHubTeamMember[]>;
  failingTeamID?: string;
}): { deps: Deps; state: FakeState } {
  const state: FakeState = {
    projects: opts.projects ?? [PROJECT_WITH_TEAMS],
    members: opts.members ?? [],
    auditLog: [],
  };
  const adminClient = fakeAdminClient(state);

  const deps: Deps = {
    adminClient: () => adminClient,
    listTeamMembers: (teamId: string) => {
      if (teamId === opts.failingTeamID) {
        return Promise.reject(new Error("GitHub API rate limited"));
      }
      return Promise.resolve(opts.teamMembers?.[teamId] ?? []);
    },
    buildGithubUserIDMap: () =>
      Promise.resolve(opts.githubUserIDMap ?? new Map()),
  };
  return { deps, state };
}

function req(auth = CRON_SECRET): Request {
  return new Request("http://local/functions/v1/sync-github-teams", {
    method: "POST",
    headers: auth ? { "x-cron-secret": auth } : {},
  });
}

function withCronSecret(fn: () => Promise<void>) {
  return async () => {
    Deno.env.set("CRON_SECRET", CRON_SECRET);
    try {
      await fn();
    } finally {
      Deno.env.delete("CRON_SECRET");
    }
  };
}

Deno.test(
  "rejects non-POST",
  withCronSecret(async () => {
    const { deps } = fakeDeps({});
    const res = await handle(
      new Request("http://local", { method: "GET" }),
      deps,
    );
    assertEquals(res.status, 405);
  }),
);

Deno.test(
  "rejects a missing cron secret",
  withCronSecret(async () => {
    const { deps } = fakeDeps({});
    const res = await handle(req(""), deps);
    assertEquals(res.status, 401);
  }),
);

Deno.test(
  "rejects a wrong cron secret",
  withCronSecret(async () => {
    const { deps } = fakeDeps({});
    const res = await handle(req("wrong-secret"), deps);
    assertEquals(res.status, 401);
  }),
);

Deno.test("rejects every request when CRON_SECRET isn't configured at all", async () => {
  const { deps } = fakeDeps({});
  const res = await handle(req(CRON_SECRET), deps);
  assertEquals(res.status, 401);
});

Deno.test(
  "adds a new member who joined the GitHub team",
  withCronSecret(async () => {
    const { deps, state } = fakeDeps({
      githubUserIDMap: new Map([[1001, "carryon-user-1"]]),
      teamMembers: { "200": [{ id: 1001, login: "newdev" }] },
    });
    const res = await handle(req(), deps);
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body.changes, 1);
    assertEquals(state.members, [
      {
        project_id: "p1",
        user_id: "carryon-user-1",
        role: "resumer",
        source: "sync",
      },
    ]);
    assertEquals(state.auditLog.length, 1);
    assertEquals(state.auditLog[0].project_id, "p1");
  }),
);

Deno.test(
  "removes a sync-sourced member no longer on any team",
  withCronSecret(async () => {
    const { deps, state } = fakeDeps({
      members: [
        {
          project_id: "p1",
          user_id: "carryon-user-1",
          role: "viewer",
          source: "sync",
        },
      ],
      githubUserIDMap: new Map(),
      teamMembers: {},
    });
    const res = await handle(req(), deps);
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body.changes, 1);
    assertEquals(state.members, []);
  }),
);

Deno.test(
  "applies a role change when a member moves between teams",
  withCronSecret(async () => {
    const { deps, state } = fakeDeps({
      members: [
        {
          project_id: "p1",
          user_id: "carryon-user-1",
          role: "viewer",
          source: "sync",
        },
      ],
      githubUserIDMap: new Map([[1001, "carryon-user-1"]]),
      teamMembers: { "300": [{ id: 1001, login: "promoted" }] }, // now on team_admin
    });
    const res = await handle(req(), deps);
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body.changes, 1);
    assertEquals(state.members[0].role, "admin");
    assertEquals(state.members[0].source, "sync");
  }),
);

Deno.test(
  "a manual override is never touched, even though GitHub disagrees",
  withCronSecret(async () => {
    const { deps, state } = fakeDeps({
      members: [
        {
          project_id: "p1",
          user_id: "carryon-user-1",
          role: "admin",
          source: "manual",
        },
      ],
      githubUserIDMap: new Map([[1001, "carryon-user-1"]]),
      teamMembers: { "100": [{ id: 1001, login: "viewer-on-github" }] }, // GitHub says viewer
    });
    const res = await handle(req(), deps);
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body.changes, 0);
    assertEquals(state.members[0].role, "admin");
    assertEquals(state.members[0].source, "manual");
  }),
);

Deno.test(
  "a manual override is never removed, even when GitHub no longer lists that user anywhere",
  withCronSecret(async () => {
    const { deps, state } = fakeDeps({
      members: [
        {
          project_id: "p1",
          user_id: "carryon-user-1",
          role: "viewer",
          source: "manual",
        },
      ],
      githubUserIDMap: new Map(),
      teamMembers: {},
    });
    const res = await handle(req(), deps);
    assertEquals(res.status, 200);
    assertEquals(state.members.length, 1);
    assertEquals(state.members[0].source, "manual");
  }),
);

Deno.test(
  "a GitHub team member with no linked Carryon account is skipped, not an error",
  withCronSecret(async () => {
    const { deps, state } = fakeDeps({
      githubUserIDMap: new Map(), // nobody mapped
      teamMembers: { "100": [{ id: 9999, login: "never-logged-in" }] },
    });
    const res = await handle(req(), deps);
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body.changes, 0);
    assertEquals(state.members, []);
  }),
);

Deno.test(
  "a failing team lookup (rate limit / outage) skips that whole project — never strips existing members",
  withCronSecret(async () => {
    const { deps, state } = fakeDeps({
      members: [
        {
          project_id: "p1",
          user_id: "carryon-user-1",
          role: "viewer",
          source: "sync",
        },
      ],
      failingTeamID: "100",
      githubUserIDMap: new Map([[1001, "carryon-user-1"]]),
      teamMembers: {},
    });
    const res = await handle(req(), deps);
    assertEquals(res.status, 200);
    const body = await res.json();
    assertEquals(body.projectsSkipped, 1);
    assertEquals(body.projectsProcessed, 0);
    assertEquals(body.changes, 0);
    // The existing member survives untouched — a rate limit never
    // empties a project's membership.
    assertEquals(state.members.length, 1);
  }),
);

Deno.test(
  "a team is only ever addressed by its stable ID — a rename changes nothing",
  withCronSecret(async () => {
    // listTeamMembers only ever receives the ID (never a slug) — if
    // this function ever started depending on a slug, this test's own
    // fake would have nothing to key off and would simply never match.
    const { deps, state } = fakeDeps({
      githubUserIDMap: new Map([[1001, "carryon-user-1"]]),
      teamMembers: { "200": [{ id: 1001, login: "same-id-after-rename" }] },
    });
    const res = await handle(req(), deps);
    assertEquals(res.status, 200);
    assertEquals(state.members[0].user_id, "carryon-user-1");
    assertEquals(state.members[0].role, "resumer");
  }),
);

Deno.test(
  "running twice with no GitHub-side change produces zero further writes (idempotent)",
  withCronSecret(async () => {
    const { deps, state } = fakeDeps({
      githubUserIDMap: new Map([[1001, "carryon-user-1"]]),
      teamMembers: { "200": [{ id: 1001, login: "steady" }] },
    });
    const res1 = await handle(req(), deps);
    const body1 = await res1.json();
    assertEquals(body1.changes, 1);

    const res2 = await handle(req(), deps);
    const body2 = await res2.json();
    assertEquals(body2.changes, 0);
    assertEquals(state.members.length, 1);
  }),
);

Deno.test(
  "a project with no team columns configured at all produces no changes",
  withCronSecret(async () => {
    const { deps } = fakeDeps({
      projects: [{
        id: "p2",
        team_viewer: null as never,
        team_resumer: null as never,
        team_admin: null as never,
      }],
    });
    const res = await handle(req(), deps);
    const body = await res.json();
    assertEquals(body.changes, 0);
  }),
);

Deno.test(
  "surfaces a project lookup failure as 500",
  withCronSecret(async () => {
    const { deps } = fakeDeps({});
    deps.adminClient = () =>
      ({
        from: () => ({
          select: () => ({
            or: () =>
              Promise.resolve({
                data: null,
                error: { code: "XXXXX", message: "boom" },
              }),
          }),
        }),
      }) as never;
    const res = await handle(req(), deps);
    assertEquals(res.status, 500);
  }),
);

Deno.test(
  "surfaces a GitHub user ID map build failure as 500",
  withCronSecret(async () => {
    const { deps } = fakeDeps({});
    deps.buildGithubUserIDMap = () => Promise.reject(new Error("boom"));
    const res = await handle(req(), deps);
    assertEquals(res.status, 500);
  }),
);
