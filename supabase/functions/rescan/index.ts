// CO-306: a nightly backstop that re-scans recently-uploaded chunks
// with the server's current rule set, which can be ahead of whatever
// rule set the laptop that originally uploaded a chunk was running
// (redaction.md's "server-side rescan is a backstop, not the primary
// defense"). On a hit: rewrite the chunk's object with the secret
// redacted (same object key, same byte_start/byte_end — those still
// describe the *original* file, same as CO-301's own laptop-side
// redaction already established — new sha256), log a `rescan_hit` row
// to audit_log, and log a structured warning naming which rule fired
// (never the matched value — redaction.md's own rule).
//
// Called by a pg_cron job via net.http_post, not a browser or the CLI
// — see supabase/migrations/20261108050000_add_secret_rescan.sql and
// config.toml's functions.rescan — authenticated by a shared
// x-cron-secret header, same shape as sync-github-teams (CO-303).
//
// Notification beyond audit_log ("notify the project admin and the
// developer... to rotate the key") is explicitly deferred: the ticket
// itself says "by email or Slack once CO-409 lands" — CO-409 (Slack
// notifications) and any email provider don't exist in this codebase
// yet, so there is nothing real to wire up without inventing a
// half-built channel ahead of its own ticket (CLAUDE.md: no
// speculative scale). The audit_log row IS the real, durable,
// queryable notification surface available today — any admin checking
// a project's audit log sees every `rescan_hit`.
//
// Batching and resumability: a chunk "needs (re)scanning" whenever
// last_rescanned_at is null, or predates redaction_config.updated_at
// (the rule set has changed since). Each chunk's own update commits as
// it's processed, so a batch that times out partway through just
// leaves the remainder — already-processed chunks no longer match the
// predicate, so neither a retried manual call nor tomorrow night's
// cron run ever re-does already-finished work, and nothing is lost if
// the run is cut short. No separate cursor/offset is needed for that
// reason alone.
//
// Reads and rewrites chunk bytes directly against Storage — the one
// deliberate, narrow exception to "bytes bypass the API" (I9,
// architecture.md) in this codebase: I9 exists to keep the server out
// of the laptop's own upload/download path, not to forbid a background
// maintenance job from operating on its own already-stored data with
// no laptop waiting on it synchronously.

import { z } from "npm:zod@4.6.7";
import { errorResponse, json } from "../_shared/http.ts";
import { adminClient as realAdminClient } from "../_shared/supabase.ts";
import { resolveBucket } from "../_shared/storage.ts";
import {
  applyDisabledRuleIDs,
  defaultRules,
  redactText,
  type Rule,
} from "../_shared/redact.ts";
import {
  compressChunk as realCompressChunk,
  decompressChunk as realDecompressChunk,
  sha256Hex as realSha256Hex,
} from "../_shared/zstd.ts";

const Input = z.object({
  // A manual full rescan (no time window, every chunk the "needs
  // rescanning" predicate still matches) — see this ticket's own
  // "triggered manually after a rule update" testing point.
  full: z.boolean().optional(),
  limit: z.number().int().positive().max(1000).optional(),
});

const DEFAULT_LIMIT = 200;
const NIGHTLY_WINDOW_HOURS = 24;

export interface Deps {
  adminClient: typeof realAdminClient;
  decompressChunk: typeof realDecompressChunk;
  compressChunk: typeof realCompressChunk;
  sha256Hex: typeof realSha256Hex;
}

const defaultDeps: Deps = {
  adminClient: realAdminClient,
  decompressChunk: realDecompressChunk,
  compressChunk: realCompressChunk,
  sha256Hex: realSha256Hex,
};

interface ChunkRow {
  session_id: string;
  stream_id: string;
  seq: number;
  object_key: string;
}

export async function handle(
  req: Request,
  deps: Deps = defaultDeps,
): Promise<Response> {
  if (req.method !== "POST") {
    return errorResponse(405, "method_not_allowed");
  }

  const cronSecret = Deno.env.get("RESCAN_CRON_SECRET");
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
  const limit = input.limit ?? DEFAULT_LIMIT;

  const admin = deps.adminClient();

  const { data: configRow, error: configErr } = await admin
    .from("redaction_config")
    .select("disabled_rule_ids, updated_at")
    .eq("id", "default")
    .maybeSingle();
  if (configErr) {
    console.error("rescan: redaction_config lookup failed", configErr.code);
    return errorResponse(500, "internal_error");
  }
  const rules: Rule[] = applyDisabledRuleIDs(
    defaultRules(),
    configRow?.disabled_rule_ids ?? [],
  );
  const rulesWatermark = configRow?.updated_at ?? new Date(0).toISOString();

  let query = admin
    .from("session_chunks")
    .select(
      "session_id, stream_id, seq, object_key, uploaded_at, last_rescanned_at",
    )
    .or(`last_rescanned_at.is.null,last_rescanned_at.lt.${rulesWatermark}`);
  if (!input.full) {
    const since = new Date(Date.now() - NIGHTLY_WINDOW_HOURS * 60 * 60 * 1000)
      .toISOString();
    query = query.gte("uploaded_at", since);
  }
  query = query.order("uploaded_at", { ascending: true }).limit(limit);

  const { data: candidates, error: candidatesErr } = await query;
  if (candidatesErr) {
    console.error("rescan: candidate chunk lookup failed", candidatesErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!candidates || candidates.length === 0) {
    return json({ scanned: 0, hits: 0 }, 200);
  }

  const sessionIDs = [
    ...new Set(candidates.map((c: ChunkRow) => c.session_id)),
  ];
  const { data: sessionRows, error: sessionsErr } = await admin
    .from("sessions")
    .select("id, project_id")
    .in("id", sessionIDs);
  if (sessionsErr) {
    console.error("rescan: session lookup failed", sessionsErr.code);
    return errorResponse(500, "internal_error");
  }
  const projectIDBySession = new Map<string, string>(
    (sessionRows ?? []).map((
      r: { id: string; project_id: string },
    ) => [r.id, r.project_id]),
  );

  const projectIDs = [...new Set([...projectIDBySession.values()])];
  const { data: projectRows, error: projectsErr } = await admin
    .from("projects")
    .select("id, bucket")
    .in("id", projectIDs);
  if (projectsErr) {
    console.error("rescan: project lookup failed", projectsErr.code);
    return errorResponse(500, "internal_error");
  }
  const bucketByProject = new Map<string, string | null>(
    (projectRows ?? []).map((
      r: { id: string; bucket: string | null },
    ) => [r.id, r.bucket]),
  );

  let scanned = 0;
  let hits = 0;

  for (const chunk of candidates as ChunkRow[]) {
    scanned++;
    const projectID = projectIDBySession.get(chunk.session_id);
    const bucket = resolveBucket({
      bucket: projectID ? bucketByProject.get(projectID) ?? null : null,
    });

    try {
      const { data: blob, error: downloadErr } = await admin.storage.from(
        bucket,
      ).download(chunk.object_key);
      if (downloadErr || !blob) {
        console.error(
          `rescan: download failed for ${bucket}/${chunk.object_key}`,
          downloadErr?.message,
        );
        continue;
      }
      const compressed = new Uint8Array(await blob.arrayBuffer());
      const plaintext = await deps.decompressChunk(compressed);
      const text = new TextDecoder().decode(plaintext);

      const { out, hits: ruleHits } = redactText(text, rules);

      if (ruleHits.length > 0) {
        const newCompressed = await deps.compressChunk(
          new TextEncoder().encode(out),
        );
        const newSha256 = await deps.sha256Hex(new TextEncoder().encode(out));

        const { error: updateObjErr } = await admin.storage.from(bucket).update(
          chunk.object_key,
          newCompressed,
          { contentType: "application/octet-stream", upsert: true },
        );
        if (updateObjErr) {
          console.error(
            `rescan: rewriting ${bucket}/${chunk.object_key} failed`,
            updateObjErr.message,
          );
          continue;
        }

        const { error: updateChunkErr } = await admin
          .from("session_chunks")
          .update({
            sha256: newSha256,
            last_rescanned_at: new Date().toISOString(),
          })
          .eq("session_id", chunk.session_id)
          .eq("stream_id", chunk.stream_id)
          .eq("seq", chunk.seq);
        if (updateChunkErr) {
          console.error(
            `rescan: updating session_chunks for ${chunk.session_id}/${chunk.stream_id}/${chunk.seq} failed`,
            updateChunkErr.code,
          );
          continue;
        }

        hits++;
        console.warn(
          `rescan: hit on session ${chunk.session_id} chunk ${chunk.stream_id}/${chunk.seq} — rules: ${
            [...new Set(ruleHits)].join(",")
          } — rotate the affected credential`,
        );

        if (projectID) {
          const { error: auditErr } = await admin.from("audit_log").insert({
            action: "rescan_hit",
            session_id: chunk.session_id,
            project_id: projectID,
          });
          if (auditErr) {
            console.error("rescan: audit log insert failed", auditErr.code);
          }
        }
      } else {
        const { error: markErr } = await admin
          .from("session_chunks")
          .update({ last_rescanned_at: new Date().toISOString() })
          .eq("session_id", chunk.session_id)
          .eq("stream_id", chunk.stream_id)
          .eq("seq", chunk.seq);
        if (markErr) {
          console.error(
            `rescan: marking ${chunk.session_id}/${chunk.stream_id}/${chunk.seq} as scanned failed`,
            markErr.code,
          );
        }
      }
    } catch (e) {
      console.error(
        `rescan: unexpected failure on ${chunk.session_id}/${chunk.stream_id}/${chunk.seq}`,
        e,
      );
    }
  }

  return json({ scanned, hits }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}
