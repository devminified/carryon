import { assertEquals } from "jsr:@std/assert@1.0.19";
import { type Deps, handle } from "./index.ts";

const CI_SECRET = "test-ci-secret";

function req(body: unknown, secret: string | null = CI_SECRET): Request {
  return new Request("http://local/functions/v1/record-compat-run", {
    method: "POST",
    headers: secret ? { "x-ci-secret": secret } : {},
    body: JSON.stringify(body),
  });
}

function withCiSecret(fn: () => Promise<void>) {
  return async () => {
    Deno.env.set("COMPAT_CI_SECRET", CI_SECRET);
    try {
      await fn();
    } finally {
      Deno.env.delete("COMPAT_CI_SECRET");
    }
  };
}

function withWebhook(url: string | undefined, fn: () => Promise<void>) {
  return async () => {
    if (url) Deno.env.set("COMPAT_CI_SLACK_WEBHOOK_URL", url);
    try {
      await fn();
    } finally {
      Deno.env.delete("COMPAT_CI_SLACK_WEBHOOK_URL");
    }
  };
}

function fakeDeps(opts: {
  // deno-lint-ignore no-explicit-any
  insertError?: any;
  onInsert?: (row: Record<string, unknown>) => void;
  onPostToSlack?: (webhookUrl: string, payload: unknown) => void;
  slackResult?: boolean;
}): Deps {
  return {
    adminClient: () =>
      ({
        from(table: string) {
          if (table !== "claude_compat_runs") {
            throw new Error(`unexpected table ${table}`);
          }
          return {
            insert: (row: Record<string, unknown>) => {
              opts.onInsert?.(row);
              return Promise.resolve({ error: opts.insertError ?? null });
            },
          };
        },
        // deno-lint-ignore no-explicit-any
      }) as any,
    postToSlack: (webhookUrl: string, payload: unknown) => {
      opts.onPostToSlack?.(webhookUrl, payload);
      return Promise.resolve(opts.slackResult ?? true);
    },
  };
}

function validBody(overrides: Record<string, unknown> = {}) {
  return {
    carryonVersion: "v0.4.0",
    claudeCodeVersion: "2.1.321",
    versionLabel: "latest",
    os: "ubuntu-latest",
    passed: true,
    ...overrides,
  };
}

Deno.test(
  "rejects non-POST",
  withCiSecret(async () => {
    const res = await handle(
      new Request("http://local", { method: "GET" }),
      fakeDeps({}),
    );
    assertEquals(res.status, 405);
  }),
);

Deno.test(
  "rejects a missing x-ci-secret header",
  withCiSecret(async () => {
    const res = await handle(req(validBody(), null), fakeDeps({}));
    assertEquals(res.status, 401);
  }),
);

Deno.test(
  "rejects a wrong x-ci-secret",
  withCiSecret(async () => {
    const res = await handle(req(validBody(), "wrong"), fakeDeps({}));
    assertEquals(res.status, 401);
  }),
);

Deno.test("rejects every request when COMPAT_CI_SECRET isn't configured at all", async () => {
  const res = await handle(req(validBody()), fakeDeps({}));
  assertEquals(res.status, 401);
});

Deno.test(
  "rejects an invalid os value",
  withCiSecret(async () => {
    const res = await handle(
      req(validBody({ os: "amiga-latest" })),
      fakeDeps({}),
    );
    assertEquals(res.status, 400);
  }),
);

Deno.test(
  "rejects an invalid versionLabel value",
  withCiSecret(async () => {
    const res = await handle(
      req(validBody({ versionLabel: "nightly" })),
      fakeDeps({}),
    );
    assertEquals(res.status, 400);
  }),
);

Deno.test(
  "records a passing run and never calls Slack",
  withCiSecret(
    withWebhook("https://hooks.slack.com/services/test", async () => {
      // deno-lint-ignore no-explicit-any
      let inserted: any = null;
      let slackCalled = false;
      const res = await handle(
        req(validBody()),
        fakeDeps({
          onInsert: (row) => (inserted = row),
          onPostToSlack: () => (slackCalled = true),
        }),
      );
      assertEquals(res.status, 200);
      assertEquals(inserted?.passed, true);
      assertEquals(inserted?.carryon_version, "v0.4.0");
      assertEquals(inserted?.version_label, "latest");
      assertEquals(slackCalled, false);
    }),
  ),
);

Deno.test(
  "records a failing run and posts to Slack when a webhook is configured",
  withCiSecret(
    withWebhook("https://hooks.slack.com/services/test", async () => {
      let slackWebhookUrl = "";
      const res = await handle(
        req(
          validBody({
            passed: false,
            failureReason: "resumed session answered incorrectly",
          }),
        ),
        fakeDeps({ onPostToSlack: (url) => (slackWebhookUrl = url) }),
      );
      assertEquals(res.status, 200);
      assertEquals(slackWebhookUrl, "https://hooks.slack.com/services/test");
    }),
  ),
);

Deno.test(
  "a failing run with no webhook configured still records, just sends no Slack message",
  withCiSecret(
    withWebhook(undefined, async () => {
      let slackCalled = false;
      const res = await handle(
        req(validBody({ passed: false, failureReason: "boom" })),
        fakeDeps({ onPostToSlack: () => (slackCalled = true) }),
      );
      assertEquals(res.status, 200);
      assertEquals(slackCalled, false);
    }),
  ),
);

Deno.test(
  "records unknownLineTypes when given",
  withCiSecret(async () => {
    // deno-lint-ignore no-explicit-any
    let inserted: any = null;
    const res = await handle(
      req(validBody({ unknownLineTypes: ["tool_progress"] })),
      fakeDeps({ onInsert: (row) => (inserted = row) }),
    );
    assertEquals(res.status, 200);
    assertEquals(inserted?.unknown_line_types, ["tool_progress"]);
  }),
);

Deno.test(
  "surfaces an insert failure as 500",
  withCiSecret(async () => {
    const res = await handle(
      req(validBody()),
      fakeDeps({ insertError: { code: "XXXXX" } }),
    );
    assertEquals(res.status, 500);
  }),
);
