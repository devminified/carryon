import { assert, assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

const CRON_SECRET = "test-cron-secret";
Deno.env.set("DIGEST_CRON_SECRET", CRON_SECRET);

interface NotificationRow {
  project_id: string;
  slack_webhook_url: string | null;
  mattermost_webhook_url?: string | null;
  notify_digest: boolean;
}

interface FakeState {
  notificationSettings: NotificationRow[];
  projects: { id: string; name: string }[];
  sessions: { project_id: string; user_id: string; last_activity_at: string }[];
  logins: Record<string, string | null>;
  // deno-lint-ignore no-explicit-any
  slackMessages: { webhookUrl: string; payload: any }[];
  // deno-lint-ignore no-explicit-any
  mattermostMessages: { webhookUrl: string; payload: any }[];
}

function freshState(overrides: Partial<FakeState> = {}): FakeState {
  return {
    notificationSettings: [],
    projects: [],
    sessions: [],
    logins: {},
    slackMessages: [],
    mattermostMessages: [],
    ...overrides,
  };
}

// deno-lint-ignore no-explicit-any
function fakeAdminClient(state: FakeState): any {
  return {
    from(table: string) {
      if (table === "project_notification_settings") {
        return {
          select: () => ({
            eq: () =>
              Promise.resolve({
                data: state.notificationSettings.filter((n) => n.notify_digest),
                error: null,
              }),
          }),
        };
      }
      if (table === "projects") {
        return {
          select: () => ({
            eq: (_col: string, id: string) => ({
              maybeSingle: () =>
                Promise.resolve({
                  data: state.projects.find((p) => p.id === id) ?? null,
                  error: null,
                }),
            }),
          }),
        };
      }
      if (table === "sessions") {
        return {
          select: () => ({
            eq: (_col: string, projectId: string) => ({
              gte: (_col2: string, since: string) =>
                Promise.resolve({
                  data: state.sessions.filter(
                    (s) =>
                      s.project_id === projectId &&
                      s.last_activity_at >= since,
                  ).map((s) => ({ user_id: s.user_id })),
                  error: null,
                }),
            }),
          }),
        };
      }
      throw new Error(`unexpected table ${table}`);
    },
    auth: {
      admin: {
        getUserById: (userId: string) =>
          Promise.resolve({
            data: {
              user: {
                user_metadata: { user_name: state.logins[userId] ?? null },
              },
            },
            error: null,
          }),
      },
    },
  };
}

function fakeDeps(
  state: FakeState,
  now = new Date("2026-06-15T13:00:00Z"),
): Deps {
  return {
    adminClient: () => fakeAdminClient(state),
    now: () => now,
    postToSlack: (webhookUrl, payload) => {
      state.slackMessages.push({ webhookUrl, payload });
      return Promise.resolve(true);
    },
    postToMattermost: (webhookUrl, payload) => {
      state.mattermostMessages.push({ webhookUrl, payload });
      return Promise.resolve(true);
    },
  };
}

function req(secret = CRON_SECRET): Request {
  return new Request("http://localhost/send-daily-digest", {
    method: "POST",
    headers: { "x-cron-secret": secret },
  });
}

Deno.test("rejects non-POST", async () => {
  const res = await handle(
    new Request("http://localhost/send-daily-digest", { method: "GET" }),
  );
  assertEquals(res.status, 405);
});

Deno.test("rejects a missing or wrong cron secret", async () => {
  const res = await handle(req("wrong"), fakeDeps(freshState()));
  assertEquals(res.status, 401);
});

Deno.test("no projects opted in: sends nothing", async () => {
  const state = freshState();
  const res = await handle(req(), fakeDeps(state));
  assertEquals(await res.json(), { projectsNotified: 0 });
  assertEquals(state.slackMessages.length, 0);
});

Deno.test("a project with a webhook but notify_digest=false is skipped", async () => {
  const state = freshState({
    notificationSettings: [
      {
        project_id: "p1",
        slack_webhook_url: "https://hooks.slack.com/x",
        notify_digest: false,
      },
    ],
    projects: [{ id: "p1", name: "carryon" }],
    sessions: [
      {
        project_id: "p1",
        user_id: "u1",
        last_activity_at: "2026-06-15T10:00:00Z",
      },
    ],
  });
  const res = await handle(req(), fakeDeps(state));
  assertEquals(await res.json(), { projectsNotified: 0 });
  assertEquals(state.slackMessages.length, 0);
});

Deno.test("a project with notify_digest=true but zero activity in the last 24h sends nothing", async () => {
  const state = freshState({
    notificationSettings: [
      {
        project_id: "p1",
        slack_webhook_url: "https://hooks.slack.com/x",
        notify_digest: true,
      },
    ],
    projects: [{ id: "p1", name: "carryon" }],
    sessions: [
      // older than the 24h window ending 2026-06-15T13:00:00Z
      {
        project_id: "p1",
        user_id: "u1",
        last_activity_at: "2026-06-13T10:00:00Z",
      },
    ],
  });
  const res = await handle(req(), fakeDeps(state));
  assertEquals(await res.json(), { projectsNotified: 0 });
  assertEquals(state.slackMessages.length, 0);
});

Deno.test("sends one message per opted-in project, grouped and sorted by session count", async () => {
  const state = freshState({
    notificationSettings: [
      {
        project_id: "p1",
        slack_webhook_url: "https://hooks.slack.com/p1",
        notify_digest: true,
      },
    ],
    projects: [{ id: "p1", name: "carryon" }],
    sessions: [
      {
        project_id: "p1",
        user_id: "u1",
        last_activity_at: "2026-06-15T10:00:00Z",
      },
      {
        project_id: "p1",
        user_id: "u2",
        last_activity_at: "2026-06-15T09:00:00Z",
      },
      {
        project_id: "p1",
        user_id: "u2",
        last_activity_at: "2026-06-15T08:00:00Z",
      },
      {
        project_id: "p1",
        user_id: "u2",
        last_activity_at: "2026-06-15T07:00:00Z",
      },
    ],
    logins: { u1: "ali", u2: "sara" },
  });
  const res = await handle(req(), fakeDeps(state));
  assertEquals(await res.json(), { projectsNotified: 1 });
  assertEquals(state.slackMessages.length, 1);
  assertEquals(state.slackMessages[0].webhookUrl, "https://hooks.slack.com/p1");
  const body = state.slackMessages[0].payload.blocks[1].text.text as string;
  // sara (3 sessions) sorts before ali (1 session).
  assert(body.indexOf("sara: 3 sessions") < body.indexOf("ali: 1 session"));
});

Deno.test("notifies multiple opted-in projects independently", async () => {
  const state = freshState({
    notificationSettings: [
      {
        project_id: "p1",
        slack_webhook_url: "https://hooks.slack.com/p1",
        notify_digest: true,
      },
      {
        project_id: "p2",
        slack_webhook_url: "https://hooks.slack.com/p2",
        notify_digest: true,
      },
    ],
    projects: [
      { id: "p1", name: "carryon" },
      { id: "p2", name: "other-repo" },
    ],
    sessions: [
      {
        project_id: "p1",
        user_id: "u1",
        last_activity_at: "2026-06-15T10:00:00Z",
      },
      {
        project_id: "p2",
        user_id: "u2",
        last_activity_at: "2026-06-15T10:00:00Z",
      },
    ],
    logins: { u1: "ali", u2: "sara" },
  });
  const res = await handle(req(), fakeDeps(state));
  assertEquals(await res.json(), { projectsNotified: 2 });
  assertEquals(state.slackMessages.length, 2);
});

Deno.test("a project with only Mattermost configured gets no Slack calls at all, and vice versa", async () => {
  const state = freshState({
    notificationSettings: [
      {
        project_id: "p1",
        slack_webhook_url: "https://hooks.slack.com/p1",
        mattermost_webhook_url: null,
        notify_digest: true,
      },
      {
        project_id: "p2",
        slack_webhook_url: null,
        mattermost_webhook_url: "http://mattermost.example/hooks/p2",
        notify_digest: true,
      },
    ],
    projects: [
      { id: "p1", name: "slack-only" },
      { id: "p2", name: "mattermost-only" },
    ],
    sessions: [
      {
        project_id: "p1",
        user_id: "u1",
        last_activity_at: "2026-06-15T10:00:00Z",
      },
      {
        project_id: "p2",
        user_id: "u2",
        last_activity_at: "2026-06-15T10:00:00Z",
      },
    ],
    logins: { u1: "ali", u2: "sara" },
  });
  const res = await handle(req(), fakeDeps(state));
  assertEquals(await res.json(), { projectsNotified: 2 });
  assertEquals(state.slackMessages.length, 1);
  assertEquals(state.slackMessages[0].webhookUrl, "https://hooks.slack.com/p1");
  assertEquals(state.mattermostMessages.length, 1);
  assertEquals(
    state.mattermostMessages[0].webhookUrl,
    "http://mattermost.example/hooks/p2",
  );
});

Deno.test("a project with both webhooks configured gets both", async () => {
  const state = freshState({
    notificationSettings: [
      {
        project_id: "p1",
        slack_webhook_url: "https://hooks.slack.com/p1",
        mattermost_webhook_url: "http://mattermost.example/hooks/p1",
        notify_digest: true,
      },
    ],
    projects: [{ id: "p1", name: "both-channels" }],
    sessions: [
      {
        project_id: "p1",
        user_id: "u1",
        last_activity_at: "2026-06-15T10:00:00Z",
      },
    ],
    logins: { u1: "ali" },
  });
  const res = await handle(req(), fakeDeps(state));
  assertEquals(await res.json(), { projectsNotified: 1 });
  assertEquals(state.slackMessages.length, 1);
  assertEquals(state.mattermostMessages.length, 1);
  const mmBody = state.mattermostMessages[0].payload.attachments[0]
    .text as string;
  assert(mmBody.includes("ali: 1 session"));
});

Deno.test("a failed webhook on one channel doesn't block the other", async () => {
  const state = freshState({
    notificationSettings: [
      {
        project_id: "p1",
        slack_webhook_url: "https://hooks.slack.com/p1",
        mattermost_webhook_url: "http://mattermost.example/hooks/p1",
        notify_digest: true,
      },
    ],
    projects: [{ id: "p1", name: "both-channels" }],
    sessions: [
      {
        project_id: "p1",
        user_id: "u1",
        last_activity_at: "2026-06-15T10:00:00Z",
      },
    ],
    logins: { u1: "ali" },
  });
  const deps = fakeDeps(state);
  deps.postToSlack = () => Promise.resolve(false); // simulated Slack outage — never throws, same as the real sender
  const res = await handle(req(), deps);
  assertEquals(await res.json(), { projectsNotified: 1 });
  assertEquals(state.mattermostMessages.length, 1);
});

Deno.test("surfaces a notification settings lookup failure as 500", async () => {
  // deno-lint-ignore no-explicit-any
  const deps: any = {
    adminClient: () => ({
      from: () => ({
        select: () => ({
          eq: () => Promise.resolve({ data: null, error: { code: "XXXXX" } }),
        }),
      }),
    }),
    now: () => new Date(),
  };
  const res = await handle(req(), deps);
  assertEquals(res.status, 500);
});

Deno.test("a sessions lookup failure for one project is logged and skipped, not fatal to the batch", async () => {
  const state = freshState({
    notificationSettings: [
      {
        project_id: "p1",
        slack_webhook_url: "https://hooks.slack.com/p1",
        notify_digest: true,
      },
      {
        project_id: "p2",
        slack_webhook_url: "https://hooks.slack.com/p2",
        notify_digest: true,
      },
    ],
    projects: [
      { id: "p1", name: "carryon" },
      { id: "p2", name: "other-repo" },
    ],
    sessions: [
      {
        project_id: "p2",
        user_id: "u2",
        last_activity_at: "2026-06-15T10:00:00Z",
      },
    ],
    logins: { u2: "sara" },
  });
  const deps = fakeDeps(state);
  // deno-lint-ignore no-explicit-any
  const realAdmin = deps.adminClient() as any;
  deps.adminClient = () =>
    ({
      ...realAdmin,
      from(table: string) {
        if (table === "sessions") {
          return {
            select: () => ({
              eq: (_c: string, projectId: string) => ({
                gte: () => {
                  if (projectId === "p1") {
                    return Promise.resolve({
                      data: null,
                      error: { code: "XXXXX" },
                    });
                  }
                  return Promise.resolve({
                    data: [{ user_id: "u2" }],
                    error: null,
                  });
                },
              }),
            }),
          };
        }
        return realAdmin.from(table);
      },
      // deno-lint-ignore no-explicit-any
    }) as any;
  const res = await handle(req(), deps);
  assertEquals(await res.json(), { projectsNotified: 1 });
  assertEquals(state.slackMessages.length, 1);
  assertEquals(state.slackMessages[0].webhookUrl, "https://hooks.slack.com/p2");
});
