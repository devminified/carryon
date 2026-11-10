import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

const CRON_SECRET = "test-cron-secret";

function req(secret: string | null = CRON_SECRET): Request {
  return new Request("http://local/functions/v1/check-observability-alerts", {
    method: "POST",
    headers: secret ? { "x-cron-secret": secret } : {},
  });
}

function withCronSecret(fn: () => Promise<void>) {
  return async () => {
    Deno.env.set("CHECK_ALERTS_CRON_SECRET", CRON_SECRET);
    try {
      await fn();
    } finally {
      Deno.env.delete("CHECK_ALERTS_CRON_SECRET");
    }
  };
}

function withWebhook(url: string | undefined, fn: () => Promise<void>) {
  return async () => {
    if (url) Deno.env.set("OBSERVABILITY_SLACK_WEBHOOK_URL", url);
    try {
      await fn();
    } finally {
      Deno.env.delete("OBSERVABILITY_SLACK_WEBHOOK_URL");
    }
  };
}

type RpcName =
  | "project_storage_totals"
  | "stale_agents_on_a_workday"
  | "lag_p95_seconds"
  | "fleet_upload_error_rate"
  | "abnormal_storage_growth_project_ids";

// The real `observability_alert_state` table is read two different
// ways in index.ts: a single alert's current state
// (.eq("alert_key", key).maybeSingle()) and the full list of
// currently-firing alert keys under a given prefix
// (.eq("is_firing", true).like("alert_key", "<prefix>:%")), once for
// stale_agent:* and once for storage_growth:*. This fake's .eq()
// branches on which column it was given, and its .like() branches on
// the prefix, same as a real Postgres planner would just answer
// either query — simpler than three separate fakes.
function fakeDeps(opts: {
  rpc?: Partial<Record<RpcName, { data?: unknown; error?: unknown }>>;
  existingAlertState?: Record<string, boolean>; // alert_key -> is_firing
  firingGrowthAlertKeys?: string[];
  firingStaleAgentAlertKeys?: string[];
  onAlertStateWrite?: (row: Record<string, unknown>) => void;
  onSnapshotUpsert?: (row: Record<string, unknown>) => void;
  onPostToSlack?: (summary: string) => void;
}): Deps {
  const alertState = new Map(Object.entries(opts.existingAlertState ?? {}));

  return {
    today: () => "2026-11-12",
    adminClient: () =>
      ({
        rpc: (name: RpcName) => {
          const override = opts.rpc?.[name];
          if (override) return Promise.resolve(override);
          const defaults: Record<RpcName, { data: unknown; error: null }> = {
            project_storage_totals: { data: [], error: null },
            stale_agents_on_a_workday: { data: [], error: null },
            lag_p95_seconds: { data: 0, error: null },
            fleet_upload_error_rate: { data: 0, error: null },
            abnormal_storage_growth_project_ids: { data: [], error: null },
          };
          return Promise.resolve(defaults[name]);
        },
        from(table: string) {
          if (table === "storage_snapshots") {
            return {
              upsert: (row: Record<string, unknown>) => {
                opts.onSnapshotUpsert?.(row);
                return Promise.resolve({ error: null });
              },
            };
          }
          if (table === "observability_alert_state") {
            return {
              select(_cols: string) {
                return {
                  eq(col: string, value: unknown) {
                    if (col === "alert_key") {
                      return {
                        maybeSingle: () =>
                          Promise.resolve({
                            data: alertState.has(value as string)
                              ? { is_firing: alertState.get(value as string) }
                              : null,
                            error: null,
                          }),
                      };
                    }
                    // col === "is_firing" — one of the two "currently
                    // firing under this prefix" listing paths.
                    return {
                      like: (_c: string, pattern: string) => {
                        const keys = pattern.startsWith("stale_agent")
                          ? opts.firingStaleAgentAlertKeys ?? []
                          : opts.firingGrowthAlertKeys ?? [];
                        return Promise.resolve({
                          data: keys.map((k) => ({ alert_key: k })),
                          error: null,
                        });
                      },
                    };
                  },
                };
              },
              upsert: (row: Record<string, unknown>) => {
                alertState.set(
                  row.alert_key as string,
                  row.is_firing as boolean,
                );
                opts.onAlertStateWrite?.(row);
                return Promise.resolve({ error: null });
              },
            };
          }
          throw new Error(`unexpected table ${table}`);
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
    postToSlack: (_url: string, payload: { text: string }) => {
      opts.onPostToSlack?.(payload.text);
      return Promise.resolve(true);
    },
  };
}

function staleAgentRow(
  overrides: Partial<
    { user_id: string; machine_id: string; hostname: string }
  > = {},
) {
  return {
    user_id: "11111111-1111-4111-8111-111111111111",
    machine_id: "m1",
    hostname: "alis-macbook",
    ...overrides,
  };
}

Deno.test("rejects non-POST", async () => {
  const res = await handle(
    new Request("http://local", { method: "GET" }),
    fakeDeps({}),
  );
  assertEquals(res.status, 405);
});

Deno.test(
  "rejects a missing x-cron-secret",
  withCronSecret(async () => {
    const res = await handle(req(null), fakeDeps({}));
    assertEquals(res.status, 401);
  }),
);

Deno.test("rejects every request when CHECK_ALERTS_CRON_SECRET isn't configured at all", async () => {
  const res = await handle(req(), fakeDeps({}));
  assertEquals(res.status, 401);
});

Deno.test(
  "a stale agent fires once on the false -> true transition, naming the machine",
  withCronSecret(
    withWebhook("https://hooks.slack.com/services/test", async () => {
      let posted = "";
      const res = await handle(
        req(),
        fakeDeps({
          rpc: {
            stale_agents_on_a_workday: { data: [staleAgentRow()], error: null },
          },
          onPostToSlack: (summary) => (posted = summary),
        }),
      );
      assertEquals(res.status, 200);
      assertEquals(posted.includes("alis-macbook"), true);
      assertEquals(posted.includes("24 hours"), true);
    }),
  ),
);

Deno.test(
  "a stale agent that was already firing does not re-notify",
  withCronSecret(
    withWebhook("https://hooks.slack.com/services/test", async () => {
      let slackCalled = false;
      const row = staleAgentRow();
      const res = await handle(
        req(),
        fakeDeps({
          rpc: { stale_agents_on_a_workday: { data: [row], error: null } },
          existingAlertState: {
            [`stale_agent:${row.user_id}:${row.machine_id}`]: true,
          },
          firingStaleAgentAlertKeys: [
            `stale_agent:${row.user_id}:${row.machine_id}`,
          ],
          onPostToSlack: () => (slackCalled = true),
        }),
      );
      assertEquals(res.status, 200);
      assertEquals(slackCalled, false);
    }),
  ),
);

Deno.test(
  "a stale agent that recovers (true -> false) clears its own alert without posting",
  withCronSecret(
    withWebhook("https://hooks.slack.com/services/test", async () => {
      let slackCalled = false;
      let clearedKey = "";
      const row = staleAgentRow();
      const key = `stale_agent:${row.user_id}:${row.machine_id}`;
      const res = await handle(
        req(),
        fakeDeps({
          rpc: { stale_agents_on_a_workday: { data: [], error: null } },
          existingAlertState: { [key]: true },
          firingStaleAgentAlertKeys: [key],
          onPostToSlack: () => (slackCalled = true),
          onAlertStateWrite: (row) => {
            if (row.is_firing === false) clearedKey = row.alert_key as string;
          },
        }),
      );
      assertEquals(res.status, 200);
      assertEquals(slackCalled, false);
      assertEquals(clearedKey, key);
    }),
  ),
);

Deno.test(
  "two different agents going stale at once each get their own alert key",
  withCronSecret(
    withWebhook("https://hooks.slack.com/services/test", async () => {
      const posted: string[] = [];
      const rowA = staleAgentRow({ machine_id: "m-a", hostname: "a-laptop" });
      const rowB = staleAgentRow({ machine_id: "m-b", hostname: "b-laptop" });
      const res = await handle(
        req(),
        fakeDeps({
          rpc: {
            stale_agents_on_a_workday: { data: [rowA, rowB], error: null },
          },
          onPostToSlack: (summary) => posted.push(summary),
        }),
      );
      assertEquals(res.status, 200);
      assertEquals(posted.length, 2);
      assertEquals(posted.some((p) => p.includes("a-laptop")), true);
      assertEquals(posted.some((p) => p.includes("b-laptop")), true);
    }),
  ),
);

Deno.test(
  "lag p95 over 10 minutes fires",
  withCronSecret(
    withWebhook("https://hooks.slack.com/services/test", async () => {
      let posted = "";
      const res = await handle(
        req(),
        fakeDeps({
          rpc: { lag_p95_seconds: { data: 900, error: null } }, // 15 minutes
          onPostToSlack: (summary) => (posted = summary),
        }),
      );
      assertEquals(res.status, 200);
      assertEquals(posted.includes("lag p95"), true);
    }),
  ),
);

Deno.test(
  "lag p95 under 10 minutes does not fire",
  withCronSecret(
    withWebhook("https://hooks.slack.com/services/test", async () => {
      let slackCalled = false;
      const res = await handle(
        req(),
        fakeDeps({
          rpc: { lag_p95_seconds: { data: 300, error: null } }, // 5 minutes
          onPostToSlack: () => (slackCalled = true),
        }),
      );
      assertEquals(res.status, 200);
      assertEquals(slackCalled, false);
    }),
  ),
);

Deno.test(
  "a null lag_p95 (no heartbeats yet) does not fire",
  withCronSecret(
    withWebhook("https://hooks.slack.com/services/test", async () => {
      let slackCalled = false;
      const res = await handle(
        req(),
        fakeDeps({
          rpc: { lag_p95_seconds: { data: null, error: null } },
          onPostToSlack: () => (slackCalled = true),
        }),
      );
      assertEquals(res.status, 200);
      assertEquals(slackCalled, false);
    }),
  ),
);

Deno.test(
  "error rate over 5% fires",
  withCronSecret(
    withWebhook("https://hooks.slack.com/services/test", async () => {
      let posted = "";
      const res = await handle(
        req(),
        fakeDeps({
          rpc: { fleet_upload_error_rate: { data: 0.1, error: null } },
          onPostToSlack: (summary) => (posted = summary),
        }),
      );
      assertEquals(res.status, 200);
      assertEquals(posted.includes("error rate"), true);
    }),
  ),
);

Deno.test(
  "a growing project fires with a per-project alert key",
  withCronSecret(
    withWebhook("https://hooks.slack.com/services/test", async () => {
      let posted = "";
      const res = await handle(
        req(),
        fakeDeps({
          rpc: {
            abnormal_storage_growth_project_ids: {
              data: ["33333333-3333-3333-3333-333333333333"],
              error: null,
            },
          },
          onPostToSlack: (summary) => (posted = summary),
        }),
      );
      assertEquals(res.status, 200);
      assertEquals(
        posted.includes("33333333-3333-3333-3333-333333333333"),
        true,
      );
    }),
  ),
);

Deno.test(
  "a project that stops growing clears its own alert without posting",
  withCronSecret(
    withWebhook("https://hooks.slack.com/services/test", async () => {
      let slackCalled = false;
      let clearedKey = "";
      const res = await handle(
        req(),
        fakeDeps({
          rpc: {
            abnormal_storage_growth_project_ids: { data: [], error: null },
          },
          firingGrowthAlertKeys: [
            "storage_growth:33333333-3333-3333-3333-333333333333",
          ],
          existingAlertState: {
            "storage_growth:33333333-3333-3333-3333-333333333333": true,
          },
          onPostToSlack: () => (slackCalled = true),
          onAlertStateWrite: (row) => {
            if (row.is_firing === false) clearedKey = row.alert_key as string;
          },
        }),
      );
      assertEquals(res.status, 200);
      assertEquals(slackCalled, false);
      assertEquals(
        clearedKey,
        "storage_growth:33333333-3333-3333-3333-333333333333",
      );
    }),
  ),
);

Deno.test(
  "snapshots today's real storage totals via project_storage_totals",
  withCronSecret(async () => {
    // deno-lint-ignore no-explicit-any
    let snapshotted: any = null;
    const res = await handle(
      req(),
      fakeDeps({
        rpc: {
          project_storage_totals: {
            data: [{
              project_id: "33333333-3333-3333-3333-333333333333",
              total_bytes: 12345,
            }],
            error: null,
          },
        },
        onSnapshotUpsert: (row) => (snapshotted = row),
      }),
    );
    assertEquals(res.status, 200);
    assertEquals(
      snapshotted?.project_id,
      "33333333-3333-3333-3333-333333333333",
    );
    assertEquals(snapshotted?.total_bytes, 12345);
    assertEquals(snapshotted?.snapshotted_at, "2026-11-12");
  }),
);

Deno.test(
  "a failing RPC for one condition never blocks the others",
  withCronSecret(
    withWebhook("https://hooks.slack.com/services/test", async () => {
      let posted = "";
      const res = await handle(
        req(),
        fakeDeps({
          rpc: {
            stale_agents_on_a_workday: {
              data: null,
              error: { code: "XXXXX" },
            },
            lag_p95_seconds: { data: 900, error: null },
          },
          onPostToSlack: (summary) => (posted = summary),
        }),
      );
      assertEquals(res.status, 200);
      assertEquals(posted.includes("lag p95"), true);
    }),
  ),
);

Deno.test(
  "with no webhook configured, an alert still transitions and records state, just sends no Slack message",
  withCronSecret(async () => {
    let slackCalled = false;
    let wroteFiringTrue = false;
    const row = staleAgentRow();
    const res = await handle(
      req(),
      fakeDeps({
        rpc: { stale_agents_on_a_workday: { data: [row], error: null } },
        onPostToSlack: () => (slackCalled = true),
        onAlertStateWrite: (writtenRow) => {
          if (
            writtenRow.alert_key ===
              `stale_agent:${row.user_id}:${row.machine_id}` &&
            writtenRow.is_firing === true
          ) {
            wroteFiringTrue = true;
          }
        },
      }),
    );
    assertEquals(res.status, 200);
    assertEquals(slackCalled, false);
    assertEquals(wroteFiringTrue, true);
  }),
);
