# CO-305 — Storage hardening: security checklist

This is CO-305's validation-point deliverable: "security checklist signed:
private buckets, TTL, region and encryption confirmed." **Needs explicit
CTO sign-off before this is considered fully done** (region, specifically
— see below) — this file states what was built and verified; it isn't
itself the sign-off.

| Item | Status | How verified |
| --- | --- | --- |
| Buckets are private | ✅ Confirmed | `carryon-sessions` and a freshly-created dedicated bucket both have `public: false` (`GET /storage/v1/bucket/<id>`); a real `GET /storage/v1/object/public/<bucket>/<key>` against the shared bucket is rejected, not served — `cmd/carryon.TestPrivateBucketHasNoPublicURL`. |
| Download signed URLs expire, 5 minutes or less | ✅ Confirmed | `get-manifest`'s `signDownloadToken` requests a 5-minute `expiresIn` (unchanged by this ticket — see `.claude/rules/redaction.md`). The underlying expiry *mechanism* (Storage's own JWT `exp` check) is verified for real with a 1-second TTL rather than waiting 300+ seconds in CI: a freshly-signed URL succeeds immediately, the identical URL is rejected once its `exp` has passed — `cmd/carryon.TestSignedDownloadURLGenuinelyExpires`. |
| Upload signed URLs | ✅ Documented platform exception, pre-existing | `createSignedUploadUrl` has a **fixed 2-hour** expiry with no `expiresIn` option — confirmed against a live instance in CO-110/CO-301 (both the SDK and the raw endpoint ignore an `expiresIn` body field; there's no `config.toml` knob either). Not something CO-305 changes or can "fix" — it's a Supabase Storage platform limit, documented in `.claude/rules/redaction.md`'s signed-URL section, not an opt-out pattern. |
| Upload size limit enforced | ✅ Confirmed | `carryon-sessions.file_size_limit` set to 10MiB directly via SQL (`update storage.buckets ...`, CO-305's migration) — 8MiB above `internal/upload`'s own `maxUncompressedChunkBytes` cap, leaving margin for a single oversized line or redaction inflation. A real PUT past the limit, through a real signed upload URL obtained via `register-chunk`, is rejected by Storage itself (`413 EntityTooLarge`) — `cmd/carryon.TestUploadOverSizeLimitIsRefused`. |
| Per-project bucket isolation | ✅ Confirmed | A project with a dedicated bucket (`projects.bucket`) has its chunk bytes land there, verifiably absent from the shared default bucket (`storage.objects.bucket_id` checked directly), and a real pull through that project succeeds using the dedicated bucket end to end — `cmd/carryon.TestDedicatedBucketIsolation`. The RLS boundary (`resumer_can_read_session_objects`) was generalized to drop its CO-302-era hardcoded `bucket_id = 'carryon-sessions'` condition — the real tenant-isolation boundary was always the path-derived `project_id` via `has_role()`/`storage_object_project_id()`, never the bucket name, so a project's own bucket doesn't bypass it. |
| Bucket creation / dedicated-bucket assignment | ✅ By design, manual | Creating a dedicated bucket and setting `projects.bucket` are deliberate one-time ops actions, not automated by any Edge Function — the same pattern already used for the GitHub OAuth App and GitHub App setup (`.claude/rules/auth.md`). Nothing to sign off here beyond confirming that's the intended shape, not a gap. |
| Encryption at rest | ✅ Nothing to configure | Supabase Storage's platform default (S3-backed, encrypted at rest) — not a Carryon-side setting, same as any other Supabase Storage bucket in this project. |
| Region confirmation | ⚠️ **Needs CTO/human input** | Which Supabase Cloud region `carryon-staging`/`carryon-prod` actually run in (and whether that satisfies any client data-residency requirement) is an operational, per-deployment decision — not something a migration or a local test can verify. Flagging this the same way CO-302's role matrix flagged CTO sign-off: **this line is not yet checked off**, pending the user's own answer once cloud setup happens. |

## What this checklist does NOT cover

Per the user's explicit instruction mid-Phase-3: no real secrets or
credentials were configured against Supabase Cloud/staging as part of
this ticket. Every row above was verified against local Supabase only
(`127.0.0.1:54421`/`:54422`). The region-confirmation row, and any cloud-
side bucket/region verification, is deferred to the user's own Phase 3
cloud testing pass — not something this ticket could complete standing
on local infrastructure.
