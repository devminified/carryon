// CO-303: reconciles project_members with each project's GitHub teams
// (team_viewer/team_resumer/team_admin, stored by stable numeric team
// ID — see _shared/github.ts for why a rename never breaks this).
// Runs every 15 minutes via a pg_cron job calling this function over
// HTTP (supabase/migrations/20261108020000_add_github_team_sync.sql);
// there's no caller JWT, so Kong's JWT gate is disabled for this one
// function (config.toml) and the request is authenticated here instead
// against a shared secret header.
//
// Never touches a project_members row with source = 'manual' — those
// are an admin's own direct action and survive every sync by
// construction, because this job only ever writes or removes rows it
// itself created (source = 'sync').
//
// If listing a project's team members fails for any reason (GitHub API
// outage, rate limit, a bad team ID), that project is skipped entirely
// for this run — never partially reconciled, and never treated as "the
// team came back empty," which would otherwise strip everyone's access
// over a transient failure.

import { errorResponse, json } from "../_shared/http.ts";
import {
  type GitHubTeamMember,
  realListTeamMembers,
} from "../_shared/github.ts";
import { adminClient as realAdminClient } from "../_shared/supabase.ts";

type Role = "viewer" | "resumer" | "admin";

// Checked highest-first: a GitHub user who's in both team_admin and
// team_viewer gets admin, matching project_role's own ordering.
const ROLE_COLUMNS: {
  role: Role;
  column: "team_admin" | "team_resumer" | "team_viewer";
}[] = [
  { role: "admin", column: "team_admin" },
  { role: "resumer", column: "team_resumer" },
  { role: "viewer", column: "team_viewer" },
];

export interface Deps {
  adminClient: typeof realAdminClient;
  listTeamMembers: (teamId: string) => Promise<GitHubTeamMember[]>;
  // Maps every Carryon account with a linked GitHub identity to that
  // identity's numeric GitHub user ID — built once per invocation, not
  // once per team member, since both admin API calls this needs
  // (listUsers, then getUserById per user — see realBuildGithubUserIDMap)
  // are already paid for the whole run, not per lookup.
  buildGithubUserIDMap: () => Promise<Map<number, string>>;
}

// listUsers() doesn't eagerly load each user's identities (verified
// against a live local instance — a user's "identities" field comes
// back null from that endpoint); getUserById() does. There's no single
// admin API call that returns every user's GitHub identity in one
// shot, so this pays one getUserById per Carryon user — fine at this
// project's actual scale (CLAUDE.md: no speculative scale; a handful
// of developers, not thousands).
async function realBuildGithubUserIDMap(): Promise<Map<number, string>> {
  const admin = realAdminClient();
  const map = new Map<number, string>();
  const perPage = 200;
  for (let page = 1;; page++) {
    const { data, error } = await admin.auth.admin.listUsers({
      page,
      perPage,
    });
    if (error) throw error;
    if (data.users.length === 0) break;

    for (const u of data.users) {
      const { data: full, error: fullErr } = await admin.auth.admin
        .getUserById(u.id);
      if (fullErr || !full?.user) continue;
      for (const identity of full.user.identities ?? []) {
        if (identity.provider !== "github") continue;
        // identity.id is GoTrue's own stable field for the provider's
        // user id (auth.identities.provider_id) — verified against a
        // live local instance, not assumed from the provider-specific
        // identity_data payload shape.
        const githubID = Number(identity.id);
        if (Number.isFinite(githubID)) map.set(githubID, u.id);
      }
    }
    if (data.users.length < perPage) break;
  }
  return map;
}

const defaultDeps: Deps = {
  adminClient: realAdminClient,
  listTeamMembers: realListTeamMembers,
  buildGithubUserIDMap: realBuildGithubUserIDMap,
};

interface ProjectRow {
  id: string;
  team_viewer: string | null;
  team_resumer: string | null;
  team_admin: string | null;
}

interface MemberRow {
  user_id: string;
  role: Role;
  source: "manual" | "sync";
}

export async function handle(
  req: Request,
  deps: Deps = defaultDeps,
): Promise<Response> {
  if (req.method !== "POST") {
    return errorResponse(405, "method_not_allowed");
  }

  const cronSecret = Deno.env.get("CRON_SECRET");
  if (!cronSecret || req.headers.get("x-cron-secret") !== cronSecret) {
    return errorResponse(401, "unauthorized");
  }

  const admin = deps.adminClient();

  const { data: projectRows, error: projectsErr } = await admin
    .from("projects")
    .select("id, team_viewer, team_resumer, team_admin")
    .or(
      "team_viewer.not.is.null,team_resumer.not.is.null,team_admin.not.is.null",
    );
  if (projectsErr) {
    console.error("sync-github-teams: project lookup failed", projectsErr.code);
    return errorResponse(500, "internal_error");
  }
  if (!projectRows || projectRows.length === 0) {
    return json({ projectsProcessed: 0, projectsSkipped: 0, changes: 0 }, 200);
  }

  let githubUserIDMap: Map<number, string>;
  try {
    githubUserIDMap = await deps.buildGithubUserIDMap();
  } catch (e) {
    console.error(
      "sync-github-teams: building the GitHub user ID map failed",
      e,
    );
    return errorResponse(500, "internal_error");
  }

  let projectsProcessed = 0;
  let projectsSkipped = 0;
  let changes = 0;

  for (const project of projectRows as ProjectRow[]) {
    const desiredByGithubID = new Map<number, Role>();
    let failed = false;

    for (const { role, column } of ROLE_COLUMNS) {
      const teamId = project[column];
      if (!teamId) continue;
      try {
        const members = await deps.listTeamMembers(teamId);
        for (const m of members) {
          if (!desiredByGithubID.has(m.id)) desiredByGithubID.set(m.id, role);
        }
      } catch (e) {
        console.error(
          `sync-github-teams: listing ${column} (${teamId}) for project ${project.id} failed, skipping this project`,
          e,
        );
        failed = true;
        break;
      }
    }
    if (failed) {
      projectsSkipped++;
      continue;
    }
    projectsProcessed++;

    // A GitHub team member with no linked Carryon account yet has
    // nothing to grant access to until they log in — skipped, not an
    // error.
    const desiredByUserID = new Map<string, Role>();
    for (const [githubID, role] of desiredByGithubID) {
      const userID = githubUserIDMap.get(githubID);
      if (userID) desiredByUserID.set(userID, role);
    }

    const { data: existingRows, error: existingErr } = await admin
      .from("project_members")
      .select("user_id, role, source")
      .eq("project_id", project.id);
    if (existingErr) {
      console.error(
        `sync-github-teams: existing member lookup for project ${project.id} failed`,
        existingErr.code,
      );
      continue;
    }
    const existingByUserID = new Map<string, MemberRow>(
      (existingRows ?? []).map((r: MemberRow) => [r.user_id, r]),
    );

    for (const [userID, role] of desiredByUserID) {
      const existing = existingByUserID.get(userID);
      if (existing?.source === "manual") continue; // never overridden
      if (existing && existing.role === role) continue; // already correct

      const { error: upsertErr } = await admin.from("project_members")
        .upsert(
          { project_id: project.id, user_id: userID, role, source: "sync" },
          { onConflict: "project_id,user_id" },
        );
      if (upsertErr) {
        console.error(
          `sync-github-teams: upsert for project ${project.id} user ${userID} failed`,
          upsertErr.code,
        );
        continue;
      }
      changes++;
      await admin.from("audit_log").insert({ action: "role_change" });
    }

    for (const [userID, existing] of existingByUserID) {
      if (existing.source !== "sync") continue; // manual rows are never removed by this job
      if (desiredByUserID.has(userID)) continue; // still a member somewhere

      const { error: deleteErr } = await admin
        .from("project_members")
        .delete()
        .eq("project_id", project.id)
        .eq("user_id", userID);
      if (deleteErr) {
        console.error(
          `sync-github-teams: removing project ${project.id} user ${userID} failed`,
          deleteErr.code,
        );
        continue;
      }
      changes++;
      await admin.from("audit_log").insert({ action: "role_change" });
    }
  }

  return json({ projectsProcessed, projectsSkipped, changes }, 200);
}

if (import.meta.main) {
  Deno.serve((req) => handle(req));
}
