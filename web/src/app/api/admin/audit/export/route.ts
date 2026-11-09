import { NextResponse } from 'next/server';
import { requireGlobalAdmin } from '@/shared/lib/auth';
import { listAuditLog } from '@/modules/admin/data/queries';
import { auditLogToCsv } from '@/modules/admin/lib/csv';

// A Route Handler, not a Server Action, because this needs to hand
// the browser a real downloadable file — a GET with the exact same
// query params the Audit page's own filter form produces, so a plain
// <a href> export link always matches whatever's on screen (CO-410's
// own "CSV export matches the filtered view" testing point), with no
// second filter implementation to keep in sync.
export async function GET(request: Request) {
  await requireGlobalAdmin();

  const { searchParams } = new URL(request.url);
  const rows = await listAuditLog({
    actorEmail: searchParams.get('actorEmail') ?? undefined,
    action: searchParams.get('action') ?? undefined,
    projectId: searchParams.get('projectId') ?? undefined,
    since: searchParams.get('since') ?? undefined,
    until: searchParams.get('until') ?? undefined,
  });

  return new NextResponse(auditLogToCsv(rows), {
    headers: {
      'content-type': 'text/csv; charset=utf-8',
      'content-disposition': 'attachment; filename="carryon-audit-log.csv"',
    },
  });
}
