import { z } from 'zod';

// Zod v4's z.string().uuid() enforces real RFC 4122 version/variant
// nibbles, which rejects supabase/seed.sql's own deliberately simple
// fake IDs (ali is '11111111-1111-1111-1111-111111111111', sara is
// '22222222-...222222222222' — found the hard way via this ticket's
// own e2e suite: updateMemberRoleAction/removeMemberAction round-trip
// a project_members row's real user_id through a hidden form field,
// and ali's own first attempt to change sara's role 500'd with
// "Invalid UUID" even though the ID was correct). Every project_id/
// user_id this module validates ultimately just needs to be a value
// Postgres's own uuid column type accepts — which has no such
// constraint — so these use a shape-only check instead. Real
// Supabase Auth / gen_random_uuid() IDs are always strict-valid
// anyway; this only matters for local seed data.
const looseUuid = z.string().regex(/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i, 'Invalid UUID');

// Mirrors supabase/functions/_shared/remote.ts's isCanonicalRemote
// shape check — deliberately NOT a second implementation of
// internal/project.NormalizeRemote (that file's own comment explains
// why two independent URL parsers drifting apart is worse than
// asking for the canonical form directly). The form's own helper
// text asks for "host/owner/repo" so a real GitHub URL still needs a
// quick manual trim — see this ticket's own PR description for why.
const canonicalRemotePattern = /^[a-z0-9.-]+\/[a-z0-9_.-]+(?:\/[a-z0-9_.-]+)+$/;

export const registerProjectSchema = z.object({
  name: z.string().trim().min(1).max(200),
  gitRemote: z
    .string()
    .trim()
    .toLowerCase()
    .min(1)
    .max(500)
    .regex(canonicalRemotePattern, 'use the canonical host/owner/repo form, e.g. github.com/devminified/acme-api'),
});
export type RegisterProjectInput = z.infer<typeof registerProjectSchema>;

// Empty string is accepted from the form and normalized to null by
// the action — a cleared text input should mean "no team configured",
// not NaN.
const optionalTeamId = z
  .string()
  .trim()
  .transform((v) => (v === '' ? null : v))
  .refine((v) => v === null || /^\d+$/.test(v), 'must be a numeric GitHub team ID');

export const updateProjectSettingsSchema = z.object({
  projectId: looseUuid,
  bucket: z
    .string()
    .trim()
    .transform((v) => (v === '' ? null : v)),
  retentionDaysAfterClose: z.coerce.number().int().min(0).max(3650),
  closed: z.boolean(),
  dailyTokenBudget: z.coerce.number().int().min(0),
  teamViewer: optionalTeamId,
  teamResumer: optionalTeamId,
  teamAdmin: optionalTeamId,
  slackWebhookUrl: z
    .string()
    .trim()
    .transform((v) => (v === '' ? null : v))
    .refine((v) => v === null || /^https:\/\/hooks\.slack\.com\/services\//.test(v), {
      message: 'must be a real Slack incoming-webhook URL (https://hooks.slack.com/services/...)',
    }),
  notifyDigest: z.boolean(),
});
export type UpdateProjectSettingsInput = z.infer<typeof updateProjectSettingsSchema>;

export const addMemberOverrideSchema = z.object({
  projectId: looseUuid,
  email: z.string().trim().toLowerCase().email(),
  role: z.enum(['viewer', 'resumer', 'admin']),
});
export type AddMemberOverrideInput = z.infer<typeof addMemberOverrideSchema>;

export const updateMemberRoleSchema = z.object({
  projectId: looseUuid,
  userId: looseUuid,
  role: z.enum(['viewer', 'resumer', 'admin']),
});
export type UpdateMemberRoleInput = z.infer<typeof updateMemberRoleSchema>;

export const removeMemberSchema = z.object({
  projectId: looseUuid,
  userId: looseUuid,
});
export type RemoveMemberInput = z.infer<typeof removeMemberSchema>;

export const updateRedactionConfigSchema = z.object({
  disabledRuleIds: z.array(z.string()),
  denylistAdditions: z.array(z.string().trim().min(1)),
  pinnedVersion: z
    .string()
    .trim()
    .transform((v) => (v === '' ? null : v)),
});
export type UpdateRedactionConfigInput = z.infer<typeof updateRedactionConfigSchema>;
