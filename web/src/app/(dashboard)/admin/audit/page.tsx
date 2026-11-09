import { requireGlobalAdmin } from '@/shared/lib/auth';
import { listAuditLog, listDistinctAuditActions, listProjectsForPicker } from '@/modules/admin/data/queries';
import { AdminPageShell } from '@/modules/admin/ui/admin-page-shell';
import { AuditFilters } from '@/modules/admin/ui/audit-filters';
import { AuditTable } from '@/modules/admin/ui/audit-table';

export default async function AdminAuditPage({
  searchParams,
}: {
  searchParams: Promise<{ actorEmail?: string; action?: string; projectId?: string; since?: string; until?: string }>;
}) {
  await requireGlobalAdmin();
  const params = await searchParams;
  const [rows, actions, projects] = await Promise.all([
    listAuditLog({
      actorEmail: params.actorEmail,
      action: params.action,
      projectId: params.projectId,
      since: params.since,
      until: params.until,
    }),
    listDistinctAuditActions(),
    listProjectsForPicker(),
  ]);

  return (
    <AdminPageShell>
      <AuditFilters actions={actions} projects={projects} current={params} />
      <AuditTable rows={rows} />
    </AdminPageShell>
  );
}
