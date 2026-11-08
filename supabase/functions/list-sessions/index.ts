// CO-204: backs `carryon ls`. Lists every session the caller can see —
// any member of the session's project, same as auth.md's access table
// ("Sees session list: Yes" for viewer/resumer/admin alike; the stricter
// resumer+ check belongs to get-manifest, not here).
//
// CO-302 landed real RLS policies, so "can see" is now just whatever
// the sessions/projects select policies already return for the
// caller's own client — no more hand-computing accessible project IDs
// from project_members first. dev is still filtered after the fact
// because a GitHub login lives in auth.users' metadata, which isn't
// reachable from a plain sessions/projects query (the auth schema isn't
// exposed to PostgREST — see auth.md) — that lookup is the one part of
// this function that still needs the service role. This is fine at this
// project's actual scale (CLAUDE.md: no speculative scale) — it would
// need real server-side filtering (a denormalized login column, or
// joining a synced table) well before FETCH_CAP below is actually tight.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import {
  adminClient as realAdminClient,
  callerClient as realCallerClient,
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
  callerClient: typeof realCallerClient;
  requireUser: typeof realRequireUser;
}

const defaultDeps: Deps = {
  adminClient: realAdminClient,
  callerClient: realCallerClient,
  requireUser: realRequireUser,
};

export async function handle(
  req: Request,
  deps: Deps = defaultDeps,
): Promise<Response> {
  if (req.method !== "POST") {
    return errorResponse(405, "method_not_allowed");
  }

  const authHeader = req.headers.get("Authorization");
  const user = await deps.requireUser(authHeader);
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

  // Every query below runs as the caller — RLS already restricts
  // "projects" and "sessions" to exactly what this user can see
  // (owns, or is a viewer/resumer/admin member of).
  const caller = deps.callerClient(authHeader!);

  const { data: projectRows, error: projectsErr } = await caller
    .from("projects")
    .select("id, name");
  if (projectsErr) {
    console.error("list-sessions: project lookup failed", projectsErr.code);
    return errorResponse(500, "internal_error");
  }

  const projectNameById = new Map<string, string>(
    (projectRows ?? []).map((
      p: { id: string; name: string },
    ) => [p.id, p.name]),
  );

  let query = caller
    .from("sessions")
    .select("id, project_id, user_id, branch, title, last_activity_at, status")
    .order("last_activity_at", { ascending: false })
    .limit(FETCH_CAP);
  if (input.project) {
    const wanted = input.project.toLowerCase();
    const candidateProjectIds = (projectRows ?? [])
      .filter((p: { name: string }) => p.name.toLowerCase() === wanted)
      .map((p: { id: string }) => p.id);
    if (candidateProjectIds.length === 0) {
      return json({ sessions: [] }, 200);
    }
    query = query.in("project_id", candidateProjectIds);
  }
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

  // auth.users' metadata isn't reachable via PostgREST/RLS at all — the
  // one lookup in this function that still needs the service role.
  const admin = deps.adminClient();
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
