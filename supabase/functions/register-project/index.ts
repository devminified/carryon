// CO-104: `carryon admin project add` calls this to register a project.
// The caller becomes the project's first member with role "admin" — there's
// no other way to bootstrap membership for a project that doesn't exist
// yet. Uses the service role (CO-102/CO-104: Edge Functions do until
// CO-302 adds real RLS policies) but identity still comes from the
// caller's own JWT, never a client-supplied field.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { isCanonicalRemote } from "../_shared/remote.ts";
import {
  adminClient as realAdminClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

const Input = z.object({
  name: z.string().min(1).max(200),
  gitRemote: z.string().min(1).max(500).refine(isCanonicalRemote, {
    message:
      "gitRemote must be the canonical host/path form (e.g. github.com/org/repo) — normalize with the CLI first",
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

  const { data: project, error: insertErr } = await admin
    .from("projects")
    .insert({ name: input.name, git_remote: input.gitRemote })
    .select()
    .single();

  if (insertErr) {
    if (insertErr.code === "23505") {
      // unique_violation on projects.git_remote
      return errorResponse(
        409,
        "conflict",
        `a project with remote ${input.gitRemote} is already registered`,
      );
    }
    console.error("register-project: insert failed", insertErr.code);
    return errorResponse(500, "internal_error");
  }

  const { error: memberErr } = await admin
    .from("project_members")
    .insert({ project_id: project.id, user_id: user.id, role: "admin" });

  if (memberErr) {
    // The project row exists but its creator isn't a member — surface
    // this plainly rather than letting it silently vanish. project.id is
    // returned so an admin can diagnose it directly (CO-302+'s roles
    // tooling, or a manual insert, in the meantime).
    console.error("register-project: member insert failed", memberErr.code);
    return errorResponse(
      500,
      "partial_failure",
      `project ${project.id} was created but adding you as its admin failed`,
    );
  }

  return json(
    { id: project.id, name: project.name, gitRemote: project.git_remote },
    201,
  );
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}
