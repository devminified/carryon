// CO-404: backs the session viewer's fork family tree — a session's
// parent, siblings, and (recursive) children. Not a recursive SQL CTE:
// a plain iterative walk up (find the root) then down (BFS the whole
// subtree) via the service role, capped generously (MAX_HOPS/MAX_NODES)
// against a hypothetical cycle or runaway tree — simpler than a new
// Postgres function for what's realistically a handful to a few dozen
// nodes per session (CLAUDE.md: no speculative scale).
//
// Gated at viewer role on the *requested* session's own project — same
// floor get-session-transcript uses. Every fork inherits its parent's
// project_id by construction (register-fork's own upsert, CO-208), so
// the whole family tree is always one project in practice; the
// `accessible` flag below is deliberately still a real per-node RLS
// check (not assumed), covering the ticket's own "forks the viewer
// can't access" case if that constraint is ever relaxed later.
import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { hasMinRole } from "../_shared/membership.ts";
import {
  adminClient as realAdminClient,
  callerClient as realCallerClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

const Input = z.object({
  sessionId: z.string().uuid(),
});

const MAX_HOPS = 50;
const MAX_NODES = 500;

type SessionRow = {
  id: string;
  user_id: string;
  title: string | null;
  started_at: string;
  parent_session_id: string | null;
};

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

// deno-lint-ignore no-explicit-any
type AnyClient = any;

const SELECT_COLS = "id,user_id,title,started_at,parent_session_id";

async function fetchById(
  admin: AnyClient,
  id: string,
): Promise<SessionRow | null> {
  const { data, error } = await admin
    .from("sessions")
    .select(SELECT_COLS)
    .eq("id", id)
    .maybeSingle();
  if (error) throw error;
  return data;
}

// Walks up from startId's own parent, stopping at the root (a null
// parent_session_id) or a dangling reference (a parent_session_id with
// no matching row — CO-307's retention/delete-session really does
// remove rows, so this is a real case, not just defensive). Returns
// the topmost *known* real row to start the down-walk from, and the
// dangling id if the chain was cut short.
async function findRoot(
  admin: AnyClient,
  self: SessionRow,
): Promise<{ root: SessionRow; danglingParentId: string | null }> {
  let current = self;
  const seen = new Set([self.id]);
  for (let i = 0; i < MAX_HOPS; i++) {
    if (!current.parent_session_id) {
      return { root: current, danglingParentId: null };
    }
    if (seen.has(current.parent_session_id)) {
      return { root: current, danglingParentId: null }; // cycle guard
    }
    const parent = await fetchById(admin, current.parent_session_id);
    if (!parent) {
      return { root: current, danglingParentId: current.parent_session_id };
    }
    seen.add(parent.id);
    current = parent;
  }
  return { root: current, danglingParentId: null };
}

async function walkDown(
  admin: AnyClient,
  root: SessionRow,
): Promise<SessionRow[]> {
  const result: SessionRow[] = [root];
  const seen = new Set([root.id]);
  let frontier = [root.id];

  while (frontier.length > 0 && result.length < MAX_NODES) {
    const { data, error } = await admin
      .from("sessions")
      .select(SELECT_COLS)
      .in("parent_session_id", frontier);
    if (error) throw error;

    const nextFrontier: string[] = [];
    for (const row of (data ?? []) as SessionRow[]) {
      if (seen.has(row.id)) continue;
      seen.add(row.id);
      result.push(row);
      nextFrontier.push(row.id);
    }
    frontier = nextFrontier;
  }
  return result;
}

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

  const { data: selfRow, error: selfErr } = await admin
    .from("sessions")
    .select(`${SELECT_COLS},project_id`)
    .eq("id", input.sessionId)
    .maybeSingle();
  if (selfErr) {
    console.error("get-session-family: session lookup failed", selfErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!selfRow) {
    return errorResponse(404, "not_found", "no such session");
  }

  const caller = deps.callerClient(req.headers.get("Authorization")!);
  const { data: memberRow, error: memberErr } = await caller
    .from("project_members")
    .select("role")
    .eq("project_id", selfRow.project_id)
    .eq("user_id", user.id)
    .maybeSingle();
  if (memberErr) {
    console.error(
      "get-session-family: membership lookup failed",
      memberErr.code,
    );
    return errorResponse(500, "internal_error");
  }
  if (!hasMinRole(memberRow?.role ?? null, "viewer")) {
    return errorResponse(403, "forbidden");
  }

  const { root, danglingParentId } = await findRoot(admin, selfRow);
  const allNodes = await walkDown(admin, root);

  // A real per-node RLS check, not an assumption — see this function's
  // own doc comment for why this can't actually trigger today, and why
  // it's still worth checking for real.
  const { data: visibleRows, error: visibleErr } = await caller
    .from("sessions")
    .select("id")
    .in("id", allNodes.map((n) => n.id));
  if (visibleErr) {
    console.error(
      "get-session-family: visibility check failed",
      visibleErr.code,
    );
    return errorResponse(500, "internal_error");
  }
  const visibleIds = new Set(
    (visibleRows ?? []).map((r: { id: string }) => r.id),
  );

  const visibleNodes = allNodes.filter((n) => visibleIds.has(n.id));
  const uniqueUserIds = [...new Set(visibleNodes.map((n) => n.user_id))];
  const loginById = new Map<string, string | null>();
  for (const uid of uniqueUserIds) {
    const { data: owner, error: ownerErr } = await admin.auth.admin.getUserById(
      uid,
    );
    if (ownerErr || !owner?.user) {
      console.error("get-session-family: owner lookup failed", ownerErr);
      loginById.set(uid, null);
      continue;
    }
    loginById.set(uid, owner.user.user_metadata?.user_name ?? null);
  }

  const nodes = allNodes.map((n) => {
    if (!visibleIds.has(n.id)) {
      return {
        id: n.id,
        parentId: n.parent_session_id,
        accessible: false as const,
      };
    }
    return {
      id: n.id,
      parentId: n.parent_session_id,
      accessible: true as const,
      title: n.title,
      ownerLogin: loginById.get(n.user_id) ?? null,
      startedAt: n.started_at,
      isCurrent: n.id === input.sessionId,
    };
  });

  return json({ nodes, danglingParentId }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}
