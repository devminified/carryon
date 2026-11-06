// CO-104: given a canonical git remote, returns the project ID if it's
// registered and the caller is a member — 404 if unregistered, 403 if
// registered but the caller isn't on it. CO-108's capture agent calls this
// (with a 1-hour cache) to decide whether a repo's sessions get uploaded
// at all. Service role for the lookups (CO-102/CO-104: until CO-302's RLS
// policies land), but membership is still enforced in code here — a
// non-member must get exactly 403, never a project ID they can't access.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { isCanonicalRemote } from "../_shared/remote.ts";
import {
  adminClient as realAdminClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

const Input = z.object({
  gitRemote: z.string().min(1).max(500).refine(isCanonicalRemote, {
    message: "gitRemote must be the canonical host/path form",
  }),
});

// Deps exists so tests can inject fakes for the two Supabase calls
// without a live instance — see index.test.ts.
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

  const { data: proj, error: projErr } = await admin
    .from("projects")
    .select("id")
    .eq("git_remote", input.gitRemote)
    .maybeSingle();

  if (projErr) {
    console.error("match-project: project lookup failed", projErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!proj) {
    return errorResponse(404, "not_found");
  }

  const { data: member, error: memberErr } = await admin
    .from("project_members")
    .select("role")
    .eq("project_id", proj.id)
    .eq("user_id", user.id)
    .maybeSingle();

  if (memberErr) {
    console.error("match-project: member lookup failed", memberErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!member) {
    return errorResponse(403, "forbidden");
  }

  return json({ projectId: proj.id, role: member.role }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}
