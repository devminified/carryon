'use server';

import { revalidatePath } from 'next/cache';
import { requireGlobalAdmin } from '@/shared/lib/auth';
import { createAdminClient } from '@/shared/lib/supabase/admin';
import { callEdgeFunction, EdgeFunctionError } from '@/shared/lib/edge-function';
import {
  addMemberOverrideSchema,
  registerProjectSchema,
  removeMemberSchema,
  updateMemberRoleSchema,
  updateProjectSettingsSchema,
  updateRedactionConfigSchema,
} from '../schemas';
import { isCompatClean } from '../lib/compat';

export type AdminActionState = { error?: string; success?: boolean } | null;

// CO-410's own "every config change writes an audit row" testing
// point, applied to every write this module makes — actor is always
// the signed-in admin who made the change (requireGlobalAdmin already
// ran before every call site below), never a client-supplied field.
async function logAdminAction(
  admin: ReturnType<typeof createAdminClient>,
  action: string,
  actorId: string,
  projectId: string | null,
  reason?: string,
): Promise<void> {
  const { error } = await admin.from('audit_log').insert({
    action,
    actor: actorId,
    project_id: projectId,
    reason: reason ?? null,
  });
  if (error) {
    // Logged, not thrown — a failed audit write must never undo an
    // otherwise-successful admin action (same "never let a side
    // effect invalidate the real result" rule CO-409's Slack sender
    // follows), but it's surfaced loudly since a missing audit row
    // for an admin change is itself worth knowing about.
    console.error('logAdminAction: audit_log insert failed', action, error.code);
  }
}

function formString(formData: FormData, key: string): string {
  return (formData.get(key) ?? '').toString();
}

export async function registerProjectAction(_prev: AdminActionState, formData: FormData): Promise<AdminActionState> {
  const user = await requireGlobalAdmin();
  const parsed = registerProjectSchema.safeParse({
    name: formString(formData, 'name'),
    gitRemote: formString(formData, 'gitRemote'),
  });
  if (!parsed.success) return { error: parsed.error.issues[0]?.message ?? 'Invalid input' };

  try {
    await callEdgeFunction('register-project', parsed.data);
  } catch (err) {
    if (err instanceof EdgeFunctionError && err.status === 409) {
      return { error: `A project with remote ${parsed.data.gitRemote} is already registered.` };
    }
    return { error: err instanceof Error ? err.message : 'Failed to register the project.' };
  }

  const admin = createAdminClient();
  await logAdminAction(admin, 'project_registered', user.id, null, `registered ${parsed.data.gitRemote}`);

  revalidatePath('/admin/projects');
  return { success: true };
}

export async function updateProjectSettingsAction(_prev: AdminActionState, formData: FormData): Promise<AdminActionState> {
  const user = await requireGlobalAdmin();
  const parsed = updateProjectSettingsSchema.safeParse({
    projectId: formString(formData, 'projectId'),
    bucket: formString(formData, 'bucket'),
    retentionDaysAfterClose: formString(formData, 'retentionDaysAfterClose'),
    closed: formData.get('closed') === 'on',
    dailyTokenBudget: formString(formData, 'dailyTokenBudget'),
    teamViewer: formString(formData, 'teamViewer'),
    teamResumer: formString(formData, 'teamResumer'),
    teamAdmin: formString(formData, 'teamAdmin'),
    slackWebhookUrl: formString(formData, 'slackWebhookUrl'),
    mattermostWebhookUrl: formString(formData, 'mattermostWebhookUrl'),
    notifyDigest: formData.get('notifyDigest') === 'on',
  });
  if (!parsed.success) return { error: parsed.error.issues[0]?.message ?? 'Invalid input' };

  const admin = createAdminClient();
  const { data: existing, error: existingErr } = await admin
    .from('projects')
    .select('closed_at')
    .eq('id', parsed.data.projectId)
    .maybeSingle();
  if (existingErr || !existing) return { error: 'Project not found.' };

  const { error: projectErr } = await admin
    .from('projects')
    .update({
      bucket: parsed.data.bucket,
      retention_days_after_close: parsed.data.retentionDaysAfterClose,
      closed_at: parsed.data.closed ? (existing.closed_at ?? new Date().toISOString()) : null,
      daily_token_budget: parsed.data.dailyTokenBudget,
      team_viewer: parsed.data.teamViewer,
      team_resumer: parsed.data.teamResumer,
      team_admin: parsed.data.teamAdmin,
    })
    .eq('id', parsed.data.projectId);
  if (projectErr) return { error: projectErr.message };

  const { error: notifErr } = await admin.from('project_notification_settings').upsert(
    {
      project_id: parsed.data.projectId,
      slack_webhook_url: parsed.data.slackWebhookUrl,
      mattermost_webhook_url: parsed.data.mattermostWebhookUrl,
      notify_digest: parsed.data.notifyDigest,
    },
    { onConflict: 'project_id' },
  );
  if (notifErr) return { error: notifErr.message };

  await logAdminAction(admin, 'project_settings_changed', user.id, parsed.data.projectId);

  revalidatePath('/admin/projects');
  return { success: true };
}

export async function addMemberOverrideAction(_prev: AdminActionState, formData: FormData): Promise<AdminActionState> {
  const user = await requireGlobalAdmin();
  const parsed = addMemberOverrideSchema.safeParse({
    projectId: formString(formData, 'projectId'),
    email: formString(formData, 'email'),
    role: formString(formData, 'role'),
  });
  if (!parsed.success) return { error: parsed.error.issues[0]?.message ?? 'Invalid input' };

  const admin = createAdminClient();
  // No direct "get user by email" in the admin API — listUsers() and a
  // client-side filter is the honest, bounded approach for a small
  // team's user base (CLAUDE.md: no speculative scale); a self-hosted
  // instance with thousands of users would need a paginated search
  // instead, not built here.
  const { data: usersPage, error: listErr } = await admin.auth.admin.listUsers({ perPage: 1000 });
  if (listErr) return { error: 'Failed to look up users.' };
  const match = usersPage.users.find((u) => u.email?.toLowerCase() === parsed.data.email);
  if (!match) return { error: `No Carryon user has signed in with ${parsed.data.email} yet.` };

  const { error: upsertErr } = await admin.from('project_members').upsert(
    { project_id: parsed.data.projectId, user_id: match.id, role: parsed.data.role, source: 'manual' },
    { onConflict: 'project_id,user_id' },
  );
  if (upsertErr) return { error: upsertErr.message };

  await logAdminAction(admin, 'role_change', user.id, parsed.data.projectId, `added ${parsed.data.email} as ${parsed.data.role}`);

  revalidatePath('/admin/members');
  return { success: true };
}

export async function updateMemberRoleAction(_prev: AdminActionState, formData: FormData): Promise<AdminActionState> {
  const user = await requireGlobalAdmin();
  const parsed = updateMemberRoleSchema.safeParse({
    projectId: formString(formData, 'projectId'),
    userId: formString(formData, 'userId'),
    role: formString(formData, 'role'),
  });
  if (!parsed.success) return { error: parsed.error.issues[0]?.message ?? 'Invalid input' };

  const admin = createAdminClient();
  const { error } = await admin
    .from('project_members')
    .update({ role: parsed.data.role, source: 'manual' })
    .eq('project_id', parsed.data.projectId)
    .eq('user_id', parsed.data.userId);
  if (error) return { error: error.message };

  await logAdminAction(admin, 'role_change', user.id, parsed.data.projectId, `changed role to ${parsed.data.role}`);

  revalidatePath('/admin/members');
  return { success: true };
}

export async function removeMemberAction(_prev: AdminActionState, formData: FormData): Promise<AdminActionState> {
  const user = await requireGlobalAdmin();
  const parsed = removeMemberSchema.safeParse({
    projectId: formString(formData, 'projectId'),
    userId: formString(formData, 'userId'),
  });
  if (!parsed.success) return { error: parsed.error.issues[0]?.message ?? 'Invalid input' };

  const admin = createAdminClient();
  const { error } = await admin
    .from('project_members')
    .delete()
    .eq('project_id', parsed.data.projectId)
    .eq('user_id', parsed.data.userId);
  if (error) return { error: error.message };

  await logAdminAction(admin, 'role_change', user.id, parsed.data.projectId, 'removed member');

  revalidatePath('/admin/members');
  return { success: true };
}

export async function updateRedactionConfigAction(_prev: AdminActionState, formData: FormData): Promise<AdminActionState> {
  const user = await requireGlobalAdmin();
  const disabledRuleIds = formString(formData, 'disabledRuleIds')
    .split(/[\n,]/)
    .map((s) => s.trim())
    .filter(Boolean);
  const denylistAdditions = formString(formData, 'denylistAdditions')
    .split(/[\n,]/)
    .map((s) => s.trim())
    .filter(Boolean);
  const parsed = updateRedactionConfigSchema.safeParse({
    disabledRuleIds,
    denylistAdditions,
    pinnedVersion: formString(formData, 'pinnedVersion'),
  });
  if (!parsed.success) return { error: parsed.error.issues[0]?.message ?? 'Invalid input' };

  const admin = createAdminClient();

  // CO-503: "admins can't move the pinned version to a version
  // without a green run" — clearing the pin (null) is always allowed,
  // since that only loosens a safety ceiling (internal/update's
  // ShouldUpdate); setting a real version requires a passing nightly
  // compatibility run across every OS and Claude Code version the
  // workflow's own matrix covers.
  if (parsed.data.pinnedVersion !== null) {
    const { data: compatRows, error: compatErr } = await admin
      .from('claude_compat_runs')
      .select('os, version_label')
      .eq('carryon_version', parsed.data.pinnedVersion)
      .eq('passed', true);
    if (compatErr) return { error: compatErr.message };
    const clean = isCompatClean(
      (compatRows ?? []).map((r) => ({ os: r.os, versionLabel: r.version_label })),
    );
    if (!clean) {
      return {
        error: `${parsed.data.pinnedVersion} has no green Claude Code compatibility run yet across every OS and Claude Code version — see the nightly compatibility workflow.`,
      };
    }
  }

  const { error } = await admin
    .from('redaction_config')
    .update({
      disabled_rule_ids: parsed.data.disabledRuleIds,
      denylist_additions: parsed.data.denylistAdditions,
      pinned_version: parsed.data.pinnedVersion,
      updated_at: new Date().toISOString(),
    })
    .eq('id', 'default');
  if (error) return { error: error.message };

  await logAdminAction(admin, 'config_change', user.id, null);

  revalidatePath('/admin/config');
  return { success: true };
}
