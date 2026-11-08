// CO-307: a daily backstop that purges a closed project's session
// data (sessions, chunks, code snapshots, and every audit_log row for
// that project) once retention_days_after_close has elapsed since
// closed_at. An active project (closed_at null) is never touched —
// there is nothing for this job to even consider until an admin
// closes a project (closed_at is currently a manual, one-time action —
// see the CO-307 migration's own comment; there's no admin UI yet to
// drive it from).
//
// Deliberately NOT part of this pass (flagged, not silently skipped):
// deleting a session's `refs/carryon/<user>/<session>` git ref on the
// developer's own remote "through the GitHub API," as the ticket
// describes. Carryon's real GitHub App (CO-303) only has org
// `Members: Read-only` — zero permission to write or delete anything
// in any repo. Doing this for real would mean expanding that live
// App's permissions to `Contents: Read & Write` across every repo
// it's installed on, which is a genuine, consequential change the
// user explicitly asked to defer rather than have made silently. Every
// other piece of a session's data (Storage bytes, DB rows, audit
// trail) IS fully purged by this job — only the hidden git ref is left
// behind for now. A stale `refs/carryon/*` ref is inert (nothing reads
// it once the session's own DB row is gone) and costs the developer's
// remote negligible storage, so this is a real but low-severity gap,
// not a correctness bug.
//
// Called by a pg_cron job via net.http_post, same shape as
// sync-github-teams (CO-303) and rescan (CO-306) — authenticated by a
// shared x-cron-secret header, no caller JWT.
//
// `{"dryRun": true}` is this ticket's own validation point: "a
// dry-run report lists what would be deleted before the first real
// run, and the CTO approves it." A dry run walks the exact same
// selection logic as a real run (same expired-project filter, same
// per-project session/chunk/snapshot lookups) but never calls
// Storage's remove() or deletes a single row — report is a
// project-by-project breakdown a human can read before the first real
// run is ever scheduled.
//
// No batching/cursor here unlike rescan (CO-306) — this ticket names
// no resumability testing point, and the number of projects that are
// both closed AND past their own retention window on any given day is
// expected to be small at this project's actual scale (no speculative
// scale, CLAUDE.md). Each project is still processed independently —
// one project's failure is logged and skipped, never aborting the
// whole run (same isolation sync-github-teams already established).

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { adminClient as realAdminClient } from "../_shared/supabase.ts";
import { resolveBucket } from "../_shared/storage.ts";

const Input = z.object({
  dryRun: z.boolean().optional(),
});

export interface Deps {
  adminClient: typeof realAdminClient;
}

const defaultDeps: Deps = { adminClient: realAdminClient };

interface ProjectRow {
  id: string;
  bucket: string | null;
  closed_at: string;
  retention_days_after_close: number;
}

interface ProjectReport {
  projectId: string;
  sessionsDeleted: number;
  objectsRemoved: number;
}

function isPastRetention(project: ProjectRow, now: Date): boolean {
  const closedAt = new Date(project.closed_at);
  const expiresAt = new Date(
    closedAt.getTime() +
      project.retention_days_after_close * 24 * 60 * 60 * 1000,
  );
  return expiresAt <= now;
}

export async function handle(
  req: Request,
  deps: Deps = defaultDeps,
): Promise<Response> {
  if (req.method !== "POST") {
    return errorResponse(405, "method_not_allowed");
  }

  const cronSecret = Deno.env.get("PURGE_CRON_SECRET");
  if (!cronSecret || req.headers.get("x-cron-secret") !== cronSecret) {
    return errorResponse(401, "unauthorized");
  }

  let input: z.infer<typeof Input>;
  try {
    input = Input.parse(await req.json().catch(() => ({})));
  } catch (e) {
    return errorResponse(
      400,
      "validation_failed",
      e instanceof Error ? e.message : String(e),
    );
  }
  const dryRun = input.dryRun === true;

  const admin = deps.adminClient();

  const { data: closedProjects, error: projectsErr } = await admin
    .from("projects")
    .select("id, bucket, closed_at, retention_days_after_close")
    .not("closed_at", "is", null);
  if (projectsErr) {
    console.error(
      "purge-expired-sessions: project lookup failed",
      projectsErr.code,
    );
    return errorResponse(500, "internal_error");
  }

  const now = new Date();
  const expired = (closedProjects ?? []).filter((p: ProjectRow) =>
    isPastRetention(p, now)
  );

  let projectsProcessed = 0;
  let sessionsDeleted = 0;
  const report: ProjectReport[] = [];

  for (const project of expired) {
    try {
      const { data: sessionRows, error: sessionsErr } = await admin
        .from("sessions")
        .select("id")
        .eq("project_id", project.id);
      if (sessionsErr) throw sessionsErr;
      const sessionIDs = (sessionRows ?? []).map((r: { id: string }) => r.id);

      let objectCount = 0;

      if (sessionIDs.length > 0) {
        const bucket = resolveBucket(project);

        const { data: chunkRows, error: chunksErr } = await admin
          .from("session_chunks")
          .select("object_key")
          .in("session_id", sessionIDs);
        if (chunksErr) throw chunksErr;

        const { data: snapshotRows, error: snapshotsErr } = await admin
          .from("code_snapshots")
          .select("object_key")
          .in("session_id", sessionIDs)
          .not("object_key", "is", null);
        if (snapshotsErr) throw snapshotsErr;

        const objectKeys = [
          ...(chunkRows ?? []).map((r: { object_key: string }) => r.object_key),
          ...(snapshotRows ?? []).map((r: { object_key: string }) =>
            r.object_key
          ),
        ];
        objectCount = objectKeys.length;

        if (!dryRun) {
          if (objectKeys.length > 0) {
            const { error: removeErr } = await admin.storage.from(bucket)
              .remove(objectKeys);
            if (removeErr) {
              console.error(
                `purge-expired-sessions: removing ${objectKeys.length} object(s) for project ${project.id} failed`,
                removeErr.message,
              );
              continue; // never delete the DB rows if the bytes might still be there
            }
          }

          // Sessions first (cascades to session_chunks/code_snapshots,
          // and sets any fork's parent_session_id to null via the
          // existing ON DELETE SET NULL FK), audit_log last — if
          // anything fails partway, this ordering never leaves the
          // audit trail gone while the data it describes still exists.
          const { error: deleteSessionsErr } = await admin
            .from("sessions")
            .delete()
            .in("id", sessionIDs);
          if (deleteSessionsErr) throw deleteSessionsErr;

          const { error: auditErr } = await admin
            .from("audit_log")
            .delete()
            .eq("project_id", project.id);
          if (auditErr) throw auditErr;
        }

        sessionsDeleted += sessionIDs.length;
      }

      report.push({
        projectId: project.id,
        sessionsDeleted: sessionIDs.length,
        objectsRemoved: objectCount,
      });
      projectsProcessed++;
    } catch (e) {
      console.error(
        `purge-expired-sessions: purging project ${project.id} failed, skipping`,
        e,
      );
    }
  }

  return json({ dryRun, projectsProcessed, sessionsDeleted, report }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}
