// CO-204: backs `carryon ls`. Lists every session the caller can see —
// any member of the session's project, same as auth.md's access table
// ("Sees session list: Yes" for viewer/resumer/admin alike; the stricter
// resumer+ check belongs to get-manifest, not here.
//
// There's no RLS to lean on yet (CO-302), so "can see" is computed here
// directly from project_members, the same stopgap every function in this
// phase uses. project/status/since are filtered in SQL; dev is filtered
// after the fact because a GitHub login lives in auth.users' metadata,
// which isn't reachable from a plain project_members/sessions query (the
// auth schema isn't exposed to PostgREST — see auth.md). This is fine at
// this project's actual scale (CLAUDE.md: no speculative scale) — it
// would need real server-side filtering (a denormalized login column, or
// joining a synced table) well before FETCH_CAP below is actually tight.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import {
  adminClient as realAdminClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

const Input = z.object({
  project: z.string().min(1).max(200).optional(),
  dev: z.string().min(1).max(200).optional(),
  status: z.enum(["live", "ended"]).optional(),
  since: z.string().datetime().optional(),
  limit: z.number().int().min(1).max(200).optional(),
});

// How many rows to pull from the database before applying the dev
// filter and the caller's requested limit — see the module doc comment
// above for why dev filtering can't happen in SQL yet.
const FETCH_CAP = 500;
const DEFAULT_LIMIT = 50;

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

  const { data: memberships, error: membershipErr } = await admin
    .from("project_members")
    .select("project_id")
    .eq("user_id", user.id);
  if (membershipErr) {
    console.error(
      "list-sessions: membership lookup failed",
      membershipErr.code,
    );
    return errorResponse(500, "internal_error");
  }

  const accessibleProjectIds = (memberships ?? []).map((
    m: { project_id: string },
  ) => m.project_id);
  if (accessibleProjectIds.length === 0) {
    return json({ sessions: [] }, 200);
  }

  const { data: projectRows, error: projectsErr } = await admin
    .from("projects")
    .select("id, name")
    .in("id", accessibleProjectIds);
  if (projectsErr) {
    console.error("list-sessions: project lookup failed", projectsErr.code);
    return errorResponse(500, "internal_error");
  }

  const projectNameById = new Map<string, string>(
    (projectRows ?? []).map((
      p: { id: string; name: string },
    ) => [p.id, p.name]),
  );

  let candidateProjectIds = accessibleProjectIds;
  if (input.project) {
    const wanted = input.project.toLowerCase();
    candidateProjectIds = (projectRows ?? [])
      .filter((p: { name: string }) => p.name.toLowerCase() === wanted)
      .map((p: { id: string }) => p.id);
    if (candidateProjectIds.length === 0) {
      return json({ sessions: [] }, 200);
    }
  }

  let query = admin
    .from("sessions")
    .select("id, project_id, user_id, branch, title, last_activity_at, status")
    .in("project_id", candidateProjectIds)
    .order("last_activity_at", { ascending: false })
    .limit(FETCH_CAP);
  if (input.status) {
    query = query.eq("status", input.status);
  }
  if (input.since) {
    query = query.gte("last_activity_at", input.since);
  }

  const { data: sessionRows, error: sessionsErr } = await query;
  if (sessionsErr) {
    console.error("list-sessions: session query failed", sessionsErr.code);
    return errorResponse(500, "internal_error");
  }

  // deno-lint-ignore no-explicit-any
  const rows: any[] = sessionRows ?? [];

  const uniqueUserIds = [...new Set(rows.map((r) => r.user_id as string))];
  const loginById = new Map<string, string | null>();
  for (const uid of uniqueUserIds) {
    const { data: owner, error: ownerErr } = await admin.auth.admin.getUserById(
      uid,
    );
    if (ownerErr || !owner?.user) {
      console.error("list-sessions: owner lookup failed", ownerErr);
      return errorResponse(500, "internal_error");
    }
    loginById.set(uid, owner.user.user_metadata?.user_name ?? null);
  }

  let filtered = rows;
  if (input.dev) {
    const wanted = input.dev.toLowerCase();
    filtered = rows.filter((r) =>
      (loginById.get(r.user_id) ?? "").toLowerCase() === wanted
    );
  }

  const limit = input.limit ?? DEFAULT_LIMIT;
  const sessions = filtered.slice(0, limit).map((r) => ({
    id: r.id,
    projectId: r.project_id,
    projectName: projectNameById.get(r.project_id) ?? "",
    ownerLogin: loginById.get(r.user_id) ?? null,
    branch: r.branch,
    title: r.title,
    lastActivityAt: r.last_activity_at,
    status: r.status,
  }));

  return json({ sessions }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}
