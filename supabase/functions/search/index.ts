// CO-501: semantic (hybrid vector + full-text) session search, behind
// the web app's search box. Embeds the query with the same gte-small
// model summarizeSession() uses for session/passage embeddings (see
// _shared/embed.ts's own comment on why), then calls the
// `search_sessions` SQL function (this ticket's own migration) via
// the CALLER's own client — never the service role — so RLS is the
// real enforcement: `search_sessions` is deliberately SECURITY
// INVOKER, not DEFINER, specifically so it can't bypass RLS the way a
// SECURITY DEFINER function run as the table owner normally would.
// See that migration's own comment for the full reasoning, and
// database-rls.md.
//
// Returns ranked session summaries (not just IDs) in one round trip —
// the matching passage snippet search_sessions already picked out,
// plus enough of each session's own row for the web result list to
// render without a second fetch per hit.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import {
  callerClient as realCallerClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";
import { embedText as realEmbedText } from "../_shared/embed.ts";

// A shape-only check, not z.string().uuid() — found for real by the
// web app's own e2e suite: Zod v4's .uuid() enforces the real RFC 4122
// version/variant nibbles, but this repo's own seed/e2e fixture
// *project* IDs (unlike fixture session IDs, which happen to use a
// valid 4/8 nibble pair) use a plain 6666...-666... pattern that
// fails that strict check even though Postgres's own `uuid` column
// accepts it fine. Every other Edge Function's `sessionId` field can
// stay strict; this is the first one validating a caller-supplied
// `projectId` directly, which is where it was actually hit.
const looseUuid =
  /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

const Input = z.object({
  query: z.string().trim().min(1).max(500),
  projectId: z.string().regex(looseUuid, "Invalid uuid").optional(),
  matchCount: z.number().int().positive().max(50).optional(),
});

export interface Deps {
  callerClient: typeof realCallerClient;
  requireUser: typeof realRequireUser;
  embedText: typeof realEmbedText;
}

const defaultDeps: Deps = {
  callerClient: realCallerClient,
  requireUser: realRequireUser,
  embedText: realEmbedText,
};

interface SearchSessionsRow {
  session_id: string;
  score: number;
  matching_passage: string | null;
}

interface SessionSummaryRow {
  id: string;
  project_id: string;
  title: string | null;
  summary: string | null;
  outcome: string | null;
  last_activity_at: string;
}

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

  let queryEmbedding: number[];
  try {
    queryEmbedding = await deps.embedText(input.query);
  } catch (e) {
    console.error(
      "search: embedding the query failed",
      e instanceof Error ? e.message : String(e),
    );
    return errorResponse(500, "internal_error");
  }

  const caller = deps.callerClient(authHeader!);
  const { data: matches, error: searchErr } = await caller.rpc(
    "search_sessions",
    {
      p_query_embedding: queryEmbedding,
      p_query_text: input.query,
      p_project_id: input.projectId ?? null,
      p_match_count: input.matchCount ?? 20,
    },
  );
  if (searchErr) {
    console.error("search: search_sessions rpc failed", searchErr.code);
    return errorResponse(500, "internal_error");
  }

  const rows = (matches ?? []) as SearchSessionsRow[];
  if (rows.length === 0) {
    return json({ results: [] }, 200);
  }

  // RLS-scoped — a row search_sessions returned is, by construction,
  // already visible to this caller, but this second query is what
  // actually gets title/summary/project_id to render; no new access
  // decision happens here.
  const { data: sessions, error: sessionsErr } = await caller
    .from("sessions")
    .select("id, project_id, title, summary, outcome, last_activity_at")
    .in("id", rows.map((r) => r.session_id));
  if (sessionsErr) {
    console.error("search: session fetch failed", sessionsErr.code);
    return errorResponse(500, "internal_error");
  }
  const sessionById = new Map(
    (sessions as SessionSummaryRow[]).map((s) => [s.id, s]),
  );

  const results = rows
    .map((r) => {
      const session = sessionById.get(r.session_id);
      if (!session) return null; // defensive: shouldn't happen under RLS
      return {
        sessionId: r.session_id,
        projectId: session.project_id,
        title: session.title,
        summary: session.summary,
        outcome: session.outcome,
        lastActivityAt: session.last_activity_at,
        score: r.score,
        matchingPassage: r.matching_passage,
      };
    })
    .filter((r): r is NonNullable<typeof r> => r !== null);

  return json({ results }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}
