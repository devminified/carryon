// CO-503: the nightly Claude Code compatibility workflow's own result
// sink — called once per (os, claude-code-version) matrix job, after
// that job's own scripted session either passes or fails. No caller
// JWT (it's GitHub Actions, not a user or even a pg_cron job — there's
// no Vault bridge to coordinate here, just a plain secret the
// workflow holds and passes directly), authenticated against a shared
// x-ci-secret header instead, same shape as every other cron-secret
// function in this codebase (see edge-functions.md).
//
// On a failing run, posts to Slack via a dedicated, team-wide
// COMPAT_CI_SLACK_WEBHOOK_URL — deliberately NOT
// project_notification_settings' own per-project webhook lookup: this
// is Carryon's own build going red, not an event belonging to any one
// client project.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { adminClient as realAdminClient } from "../_shared/supabase.ts";
import {
  buildCompatCIFailureMessage,
  postToSlackWithRetry as realPostToSlackWithRetry,
} from "../_shared/slack.ts";

const Input = z.object({
  carryonVersion: z.string().trim().min(1).max(100),
  claudeCodeVersion: z.string().trim().min(1).max(100),
  versionLabel: z.enum(["pinned", "latest"]),
  os: z.enum(["macos-latest", "ubuntu-latest", "windows-latest"]),
  passed: z.boolean(),
  failureReason: z.string().trim().max(2000).optional(),
  unknownLineTypes: z.array(z.string().trim().min(1).max(100)).max(50)
    .optional(),
  runUrl: z.string().url().optional(),
});

export interface Deps {
  adminClient: typeof realAdminClient;
  postToSlack: typeof realPostToSlackWithRetry;
}

const defaultDeps: Deps = {
  adminClient: realAdminClient,
  postToSlack: realPostToSlackWithRetry,
};

export async function handle(
  req: Request,
  deps: Deps = defaultDeps,
): Promise<Response> {
  if (req.method !== "POST") {
    return errorResponse(405, "method_not_allowed");
  }

  const ciSecret = Deno.env.get("COMPAT_CI_SECRET");
  if (!ciSecret || req.headers.get("x-ci-secret") !== ciSecret) {
    return errorResponse(401, "unauthorized");
  }

  let input: z.infer<typeof Input>;
  try {
    input = Input.parse(await req.json());
  } catch (e) {
    return errorResponse(
      400,
      "validation_failed",
      e instanceof Error ? e.message : String(e),
    );
  }

  const admin = deps.adminClient();
  const { error: insertErr } = await admin.from("claude_compat_runs").insert({
    carryon_version: input.carryonVersion,
    claude_code_version: input.claudeCodeVersion,
    version_label: input.versionLabel,
    os: input.os,
    passed: input.passed,
    failure_reason: input.failureReason ?? null,
    unknown_line_types: input.unknownLineTypes ?? [],
  });
  if (insertErr) {
    console.error("record-compat-run: insert failed", insertErr.code);
    return errorResponse(500, "internal_error");
  }

  if (!input.passed) {
    const webhookUrl = Deno.env.get("COMPAT_CI_SLACK_WEBHOOK_URL");
    if (webhookUrl) {
      const message = buildCompatCIFailureMessage({
        carryonVersion: input.carryonVersion,
        claudeCodeVersion: input.claudeCodeVersion,
        versionLabel: input.versionLabel,
        os: input.os,
        failureReason: input.failureReason ?? null,
        runUrl: input.runUrl ?? null,
      });
      await deps.postToSlack(webhookUrl, message);
    }
  }

  return json({ ok: true }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}
