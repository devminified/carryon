// CO-301: lets the laptop's redaction rule set (internal/redact) be
// narrowed or extended from the server without a new binary release.
// No request body — any authenticated user gets the same global
// config; there's no per-project redaction config in the current build
// plan (see the migration's own comment for why one row is enough).
//
// This can only disable a built-in rule by ID or add a path-denylist
// glob, never supply a new regex pattern: a compromised or careless
// server-side change can narrow what the laptop catches, but can never
// inject a pattern this binary would execute against a developer's
// own transcript content. See internal/redact's Rule doc comment.
//
// pinnedVersion (CO-308) is otherwise unrelated to redaction — it
// lives on this same row because the ticket's own instruction is to
// expose it through get-config, not a new endpoint (see the CO-308
// migration's own comment). null means no pin; internal/update's
// ShouldUpdate treats a set value as a ceiling the daily update check
// never goes past.

import { errorResponse, json } from "../_shared/http.ts";
import {
  adminClient as realAdminClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

export interface Deps {
  adminClient: typeof realAdminClient;
  requireUser: typeof realRequireUser;
}

const defaultDeps: Deps = {
  adminClient: realAdminClient,
  requireUser: realRequireUser,
};

export async function handle(
  req: Request,
  deps: Deps = defaultDeps,
): Promise<Response> {
  if (req.method !== "POST") {
    return errorResponse(405, "method_not_allowed");
  }

  const user = await deps.requireUser(req.headers.get("Authorization"));
  if (!user) {
    return errorResponse(401, "unauthorized");
  }

  const admin = deps.adminClient();
  const { data: config, error } = await admin
    .from("redaction_config")
    .select("disabled_rule_ids, denylist_additions, pinned_version")
    .eq("id", "default")
    .maybeSingle();
  if (error) {
    console.error("get-config: lookup failed", error.code);
    return errorResponse(500, "internal_error");
  }

  return json({
    disabledRuleIds: config?.disabled_rule_ids ?? [],
    denylistAdditions: config?.denylist_additions ?? [],
    pinnedVersion: config?.pinned_version ?? null,
  }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}
