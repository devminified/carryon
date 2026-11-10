import { NextResponse } from 'next/server';
import { createAdminClient } from '@/shared/lib/supabase/admin';

// CO-506: the self-hosted stack's own health-check page/endpoint — a
// self-host operator (or a monitoring probe, or docker-compose's own
// `healthcheck:` on the web service) needs one place that confirms
// "the web app can reach its own database," not just "the Next.js
// process started." Deliberately unauthenticated (no session/role
// check) and reveals nothing session-specific — only whether a
// trivial admin-client query against `projects` succeeds, same
// shallow signal a load balancer's health probe needs and nothing
// more (never echoes back table contents).
export async function GET() {
  const checks: Record<string, boolean> = {};

  try {
    const admin = createAdminClient();
    const { error } = await admin.from('projects').select('id').limit(1);
    checks.database = !error;
  } catch {
    checks.database = false;
  }

  const healthy = Object.values(checks).every(Boolean);
  return NextResponse.json(
    { status: healthy ? 'ok' : 'degraded', checks },
    { status: healthy ? 200 : 503 },
  );
}
