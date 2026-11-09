// CO-403: backs the web session viewer (/sessions/[id]). Returns the
// session's own metadata (for the side panel) plus its transcript
// chunks — every stream (the main one and any subagent streams,
// CO-109/110), each with a short-lived signed download token — so the
// browser can download and decompress them directly from Storage (I9),
// the same two-phase shape get-manifest already uses for the CLI's
// pull.
//
// Deliberately NOT get-manifest: that function requires resumer role
// (it hands out a code snapshot's download/ref too, which only
// pull/resume needs — auth.md's access table), while a viewer can read
// transcripts but never pull/resume. This function hands out transcript
// chunks only, gated at viewer role, and audits "view" (not
// "pull_manifest" — see audit_log's own migration comment naming this
// exact action for this exact ticket).
import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { hasMinRole } from "../_shared/membership.ts";
import { resolveBucket } from "../_shared/storage.ts";
import {
  adminClient as realAdminClient,
  callerClient as realCallerClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

const Input = z.object({
  sessionId: z.string().uuid(),
});

const DOWNLOAD_URL_TTL_SECONDS = 300;

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
type AdminClient = any;

// Same token-only shape as get-manifest's signDownloadToken, and the
// same reason: createSignedUrl's own signedUrl bakes in this
// function's internal SUPABASE_URL (unreachable from a browser outside
// Docker in local dev) — the browser builds its own download URL from
// its own already-correct NEXT_PUBLIC_SUPABASE_URL.
async function signDownloadToken(
  admin: AdminClient,
  bucket: string,
  objectKey: string,
): Promise<string | null> {
  const { data: signed, error } = await admin.storage
    .from(bucket)
    .createSignedUrl(objectKey, DOWNLOAD_URL_TTL_SECONDS);
  if (error || !signed) {
    console.error("get-session-transcript: signing download url failed", error);
    return null;
  }
  const token = new URL(signed.signedUrl).searchParams.get("token");
  if (!token) {
    console.error(
      "get-session-transcript: signed url had no token query param",
    );
    return null;
  }
  return token;
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

  const { data: session, error: sessionErr } = await admin
    .from("sessions")
    .select(
      "project_id, user_id, original_cwd, branch, status, outcome, blocked_reason, title, summary, summarized_at, files_touched, parent_session_id, total_bytes, started_at, last_activity_at",
    )
    .eq("id", input.sessionId)
    .maybeSingle();
  if (sessionErr) {
    console.error(
      "get-session-transcript: session lookup failed",
      sessionErr.code,
    );
    return errorResponse(500, "internal_error");
  }
  if (!session) {
    return errorResponse(404, "not_found", "no such session");
  }

  // Same "database answers, not an app-level trust" pattern get-manifest
  // moved to post-CO-302: a true non-member's query here returns zero
  // rows under RLS, not an error.
  const caller = deps.callerClient(req.headers.get("Authorization")!);
  const { data: memberRow, error: memberErr } = await caller
    .from("project_members")
    .select("role")
    .eq("project_id", session.project_id)
    .eq("user_id", user.id)
    .maybeSingle();
  if (memberErr) {
    console.error(
      "get-session-transcript: membership lookup failed",
      memberErr.code,
    );
    return errorResponse(500, "internal_error");
  }
  if (!hasMinRole(memberRow?.role ?? null, "viewer")) {
    return errorResponse(403, "forbidden");
  }

  const { data: project, error: projectErr } = await admin
    .from("projects")
    .select("name, bucket")
    .eq("id", session.project_id)
    .maybeSingle();
  if (projectErr || !project) {
    console.error(
      "get-session-transcript: project lookup failed",
      projectErr?.code,
    );
    return errorResponse(500, "internal_error");
  }
  const bucket = resolveBucket(project);

  const { data: owner, error: ownerErr } = await admin.auth.admin
    .getUserById(session.user_id);
  if (ownerErr || !owner?.user) {
    console.error("get-session-transcript: owner lookup failed", ownerErr);
    return errorResponse(500, "internal_error");
  }
  const ownerLogin = owner.user.user_metadata?.user_name ?? null;

  const { data: chunkRows, error: chunksErr } = await admin
    .from("session_chunks")
    .select("stream_id, seq, byte_start, byte_end, object_key, sha256")
    .eq("session_id", input.sessionId)
    .order("stream_id", { ascending: true })
    .order("seq", { ascending: true });
  if (chunksErr) {
    console.error("get-session-transcript: chunk list failed", chunksErr.code);
    return errorResponse(500, "internal_error");
  }

  const chunks = [];
  for (const row of chunkRows ?? []) {
    const downloadToken = await signDownloadToken(
      admin,
      bucket,
      row.object_key,
    );
    if (downloadToken === null) {
      return errorResponse(500, "internal_error");
    }
    chunks.push({
      streamId: row.stream_id,
      seq: row.seq,
      byteStart: row.byte_start,
      byteEnd: row.byte_end,
      sha256: row.sha256,
      objectKey: row.object_key,
      bucket,
      downloadToken,
    });
  }

  const { data: forkRows, error: forksErr } = await admin
    .from("sessions")
    .select("id, title, started_at")
    .eq("parent_session_id", input.sessionId)
    .order("started_at", { ascending: true });
  if (forksErr) {
    console.error("get-session-transcript: forks lookup failed", forksErr.code);
    return errorResponse(500, "internal_error");
  }

  const { error: auditErr } = await admin.from("audit_log").insert({
    actor: user.id,
    action: "view",
    session_id: input.sessionId,
    project_id: session.project_id,
    ip: req.headers.get("x-forwarded-for"),
    user_agent: req.headers.get("user-agent"),
  });
  if (auditErr) {
    // Same reasoning as get-manifest: a logging failure doesn't block a
    // read the caller is otherwise entitled to.
    console.error(
      "get-session-transcript: audit log insert failed",
      auditErr.code,
    );
  }

  return json(
    {
      session: {
        id: input.sessionId,
        projectId: session.project_id,
        projectName: project.name,
        ownerLogin,
        originalCwd: session.original_cwd,
        branch: session.branch,
        status: session.status,
        outcome: session.outcome,
        blockedReason: session.blocked_reason,
        title: session.title,
        summary: session.summary,
        summarizedAt: session.summarized_at,
        filesTouched: session.files_touched ?? [],
        parentSessionId: session.parent_session_id,
        totalBytes: session.total_bytes,
        startedAt: session.started_at,
        lastActivityAt: session.last_activity_at,
      },
      chunks,
      forks: (forkRows ?? []).map((
        f: { id: string; title: string | null; started_at: string },
      ) => ({
        id: f.id,
        title: f.title,
        startedAt: f.started_at,
      })),
    },
    200,
  );
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}
