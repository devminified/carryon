import { describe, expect, it } from 'vitest';
import {
  addMemberOverrideSchema,
  registerProjectSchema,
  removeMemberSchema,
  updateMemberRoleSchema,
  updateProjectSettingsSchema,
  updateRedactionConfigSchema,
} from './schemas';

describe('registerProjectSchema', () => {
  it('accepts a canonical host/owner/repo remote', () => {
    const result = registerProjectSchema.parse({ name: 'acme-api', gitRemote: 'github.com/devminified/acme-api' });
    expect(result.gitRemote).toBe('github.com/devminified/acme-api');
  });

  it('lowercases the remote', () => {
    const result = registerProjectSchema.parse({ name: 'acme-api', gitRemote: 'GitHub.com/DevMinified/Acme-Api' });
    expect(result.gitRemote).toBe('github.com/devminified/acme-api');
  });

  it('rejects a full https URL, not just a canonical remote', () => {
    expect(() =>
      registerProjectSchema.parse({ name: 'acme-api', gitRemote: 'https://github.com/devminified/acme-api' }),
    ).toThrow();
  });

  it('rejects a remote with no slash at all', () => {
    expect(() => registerProjectSchema.parse({ name: 'acme-api', gitRemote: 'justahostname' })).toThrow();
  });

  it('rejects an empty name', () => {
    expect(() => registerProjectSchema.parse({ name: '', gitRemote: 'github.com/a/b' })).toThrow();
  });
});

describe('updateProjectSettingsSchema', () => {
  const base = {
    projectId: '11111111-1111-4111-8111-111111111111',
    bucket: '',
    retentionDaysAfterClose: '90',
    closed: false,
    dailyTokenBudget: '200000',
    teamViewer: '',
    teamResumer: '',
    teamAdmin: '',
    slackWebhookUrl: '',
    notifyDigest: false,
  };

  it('normalizes empty-string optional fields to null', () => {
    const result = updateProjectSettingsSchema.parse(base);
    expect(result.bucket).toBeNull();
    expect(result.teamViewer).toBeNull();
    expect(result.slackWebhookUrl).toBeNull();
  });

  it('coerces numeric strings from form fields', () => {
    const result = updateProjectSettingsSchema.parse(base);
    expect(result.retentionDaysAfterClose).toBe(90);
    expect(result.dailyTokenBudget).toBe(200000);
  });

  it('rejects a non-numeric GitHub team ID', () => {
    expect(() => updateProjectSettingsSchema.parse({ ...base, teamAdmin: 'not-a-number' })).toThrow();
  });

  it('accepts a real Slack incoming-webhook URL', () => {
    const result = updateProjectSettingsSchema.parse({
      ...base,
      slackWebhookUrl: 'https://hooks.slack.com/services/T0/B0/x',
    });
    expect(result.slackWebhookUrl).toBe('https://hooks.slack.com/services/T0/B0/x');
  });

  it('rejects a URL that is not a Slack webhook', () => {
    expect(() => updateProjectSettingsSchema.parse({ ...base, slackWebhookUrl: 'https://evil.example/hook' })).toThrow();
  });
});

describe('addMemberOverrideSchema', () => {
  it('lowercases the email', () => {
    const result = addMemberOverrideSchema.parse({
      projectId: '11111111-1111-4111-8111-111111111111',
      email: 'Ali@DevMinified.com',
      role: 'viewer',
    });
    expect(result.email).toBe('ali@devminified.com');
  });

  it('rejects an invalid email', () => {
    expect(() =>
      addMemberOverrideSchema.parse({ projectId: '11111111-1111-4111-8111-111111111111', email: 'not-an-email', role: 'viewer' }),
    ).toThrow();
  });

  it('rejects an invalid role', () => {
    expect(() =>
      addMemberOverrideSchema.parse({
        projectId: '11111111-1111-4111-8111-111111111111',
        email: 'ali@devminified.com',
        role: 'superadmin',
      }),
    ).toThrow();
  });
});

describe('updateMemberRoleSchema / removeMemberSchema (loose UUID)', () => {
  // supabase/seed.sql's own real users have deliberately simple fake
  // IDs ('11111111-1111-1111-1111-111111111111') whose variant nibble
  // isn't RFC 4122-valid — Zod's own z.string().uuid() rejects them,
  // found the hard way when changing a seeded member's role 500'd
  // with "Invalid UUID" in the real e2e suite. These schemas use a
  // shape-only check instead, since that's all Postgres's own uuid
  // column type actually requires.
  it('accepts a real (if RFC-nonstrict) seeded user id', () => {
    const result = updateMemberRoleSchema.parse({
      projectId: '33333333-3333-3333-3333-333333333333',
      userId: '22222222-2222-2222-2222-222222222222',
      role: 'resumer',
    });
    expect(result.userId).toBe('22222222-2222-2222-2222-222222222222');
  });

  it('still rejects something with the wrong shape entirely', () => {
    expect(() =>
      removeMemberSchema.parse({ projectId: '33333333-3333-3333-3333-333333333333', userId: 'not-a-uuid-at-all' }),
    ).toThrow();
  });
});

describe('updateRedactionConfigSchema', () => {
  it('normalizes an empty pinnedVersion to null', () => {
    const result = updateRedactionConfigSchema.parse({ disabledRuleIds: [], denylistAdditions: [], pinnedVersion: '' });
    expect(result.pinnedVersion).toBeNull();
  });

  it('accepts a real pinned version', () => {
    const result = updateRedactionConfigSchema.parse({ disabledRuleIds: [], denylistAdditions: [], pinnedVersion: '1.4.0' });
    expect(result.pinnedVersion).toBe('1.4.0');
  });
});
