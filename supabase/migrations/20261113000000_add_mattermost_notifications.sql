-- Follow-up to CO-409 (GitHub issue #113): a project should be able
-- to configure Slack, Mattermost, or both at once, and get the same
-- three message types on whichever channel(s) it has configured.
-- One new nullable column, not a channel_type enum — both webhooks
-- can be set simultaneously, so this isn't a mutually-exclusive
-- choice. Same admin-only RLS as slack_webhook_url (database-rls.md's
-- own reasoning for why this table is admin-only in every direction:
-- a webhook URL lets its holder *post* into the team's channel, not
-- just read something) — row-level policies don't need touching for
-- a new column on an already-admin-gated row.
alter table project_notification_settings
  add column mattermost_webhook_url text;
