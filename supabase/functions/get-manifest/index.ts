// CO-203: the first step of carryon pull. Given a session_id, returns
// everything internal/pull (CO-205+) needs to rebuild it: the session's
// own metadata, its chunks in order with a short-lived signed download
// token each, and its latest code snapshot (a hidden ref name, or an
// object key and signed download token for the bundle fallback —
// CO-202).
//
// Bytes never flow through this function (I9) — it only ever hands out
// metadata and signed download tokens; internal/pull downloads chunks
// directly from Storage. Every call is audited (pull_manifest) because
// reading session data is itself access to potentially sensitive
// transcript/code content (database-rls.md's "reads of sensitive data
// are audited too").
//
// Requires resumer role or higher (CO-302 will enforce this in RLS; until
// then this function checks project_members directly, the same stopgap
// every other function in this phase uses).

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { hasMinRole, memberRole } from "../_shared/membership.ts";
import {
  adminClient as realAdminClient,
  requireUser as realRequireUser,
} from "../_shared/supabase.ts";

const Input = z.object({
  sessionId: z.string().uuid(),
});

const BUCKET = "carryon-sessions";

// Signed download URLs are capped at 5 minutes (redaction.md) — this is
// the one Storage URL shape in this codebase that genuinely needs to be
// downloaded from, not uploaded to, so createSignedUrl (not
// createSignedUploadUrl) applies and does support expiresIn.
const DOWNLOAD_URL_TTL_SECONDS = 300;

export interface Deps {
  adminClient: typeof realAdminClient;
  requireUser: typeof realRequireUser;
}

const defaultDeps: Deps = {
  adminClient: realAdminClient,
  requireUser: realRequireUser,
};

// deno-lint-ignore no-explicit-any
type AdminClient = any;

// createSignedUrl's returned signedUrl bakes in this function's own
// internal SUPABASE_URL — inside Docker (local dev) that's an internal
// hostname (http://kong:8000), unreachable from the laptop actually
// downloading the bytes. Verified against a live local instance: the
// query string's token param itself carries no host claim, just the
// object path, scope and expiry, the same thing redaction.md already
// documents for createSignedUploadUrl. So this returns only the bare
// token; the laptop client builds its own download URL from its own
// already-correct api_url (mirroring Client.StorageUploadURL).
async function signDownloadToken(
  admin: AdminClient,
  objectKey: string,
): Promise<string | null> {
  const { data: signed, error } = await admin.storage
    .from(BUCKET)
    .createSignedUrl(objectKey, DOWNLOAD_URL_TTL_SECONDS);
  if (error || !signed) {
    console.error("get-manifest: signing download url failed", error);
    return null;
  }
  const token = new URL(signed.signedUrl).searchParams.get("token");
  if (!token) {
    console.error("get-manifest: signed url had no token query param");
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
      "project_id, user_id, original_cwd, branch, head_sha, claude_code_version, status, last_activity_at",
    )
    .eq("id", input.sessionId)
    .maybeSingle();
  if (sessionErr) {
    console.error("get-manifest: session lookup failed", sessionErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!session) {
    return errorResponse(404, "not_found", "no such session");
  }

  let role: string | null;
  try {
    role = await memberRole(admin, session.project_id, user.id);
  } catch (e) {
    console.error("get-manifest: membership lookup failed", e);
    return errorResponse(500, "internal_error");
  }
  if (!hasMinRole(role, "resumer")) {
    return errorResponse(403, "forbidden");
  }

  // CO-211 needs the project's name and git remote to locate or clone a
  // local checkout before it can restore a worktree — get-manifest is
  // where every other "everything needed to rebuild this session" field
  // already lives (this function's own doc comment), so it's the
  // natural place for these too, rather than a second round trip
  // through list-sessions just for these two columns.
  const { data: project, error: projectErr } = await admin
    .from("projects")
    .select("name, git_remote")
    .eq("id", session.project_id)
    .maybeSingle();
  if (projectErr || !project) {
    console.error("get-manifest: project lookup failed", projectErr?.code);
    return errorResponse(500, "internal_error");
  }

  const { data: owner, error: ownerErr } = await admin.auth.admin
    .getUserById(session.user_id);
  if (ownerErr || !owner?.user) {
    console.error("get-manifest: owner lookup failed", ownerErr);
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
    console.error("get-manifest: chunk list failed", chunksErr.code);
    return errorResponse(500, "internal_error");
  }

  const chunks = [];
  for (const row of chunkRows ?? []) {
    const downloadToken = await signDownloadToken(admin, row.object_key);
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
      downloadToken,
    });
  }

  const { data: snapshotRow, error: snapshotErr } = await admin
    .from("code_snapshots")
    .select("seq, head_sha, tree_sha, wip_ref, object_key")
    .eq("session_id", input.sessionId)
    .order("seq", { ascending: false })
    .limit(1)
    .maybeSingle();
  if (snapshotErr) {
    console.error("get-manifest: snapshot lookup failed", snapshotErr.code);
    return errorResponse(500, "internal_error");
  }

  // deno-lint-ignore no-explicit-any
  let snapshot: Record<string, any> | null = null;
  if (snapshotRow) {
    snapshot = {
      seq: snapshotRow.seq,
      headSha: snapshotRow.head_sha,
      treeSha: snapshotRow.tree_sha,
    };
    if (snapshotRow.wip_ref) {
      snapshot.wipRef = snapshotRow.wip_ref;
    } else if (snapshotRow.object_key) {
      const downloadToken = await signDownloadToken(
        admin,
        snapshotRow.object_key,
      );
      if (downloadToken === null) {
        return errorResponse(500, "internal_error");
      }
      snapshot.objectKey = snapshotRow.object_key;
      snapshot.downloadToken = downloadToken;
    }
  }

  const { error: auditErr } = await admin.from("audit_log").insert({
    actor: user.id,
    action: "pull_manifest",
    session_id: input.sessionId,
  });
  if (auditErr) {
    // The manifest itself is still correct and safe to return — losing
    // an audit row is a logging failure, not a reason to fail the pull.
    console.error("get-manifest: audit log insert failed", auditErr.code);
  }

  return json(
    {
      session: {
        id: input.sessionId,
        projectId: session.project_id,
        projectName: project.name,
        gitRemote: project.git_remote,
        ownerLogin,
        originalCwd: session.original_cwd,
        branch: session.branch,
        headSha: session.head_sha,
        claudeCodeVersion: session.claude_code_version,
        status: session.status,
        lastActivityAt: session.last_activity_at,
      },
      chunks,
      snapshot,
    },
    200,
  );
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}
