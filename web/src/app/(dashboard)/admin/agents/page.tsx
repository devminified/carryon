import { requireGlobalAdmin } from '@/shared/lib/auth';
import { listAgentHeartbeats } from '@/modules/admin/data/queries';
import { AdminPageShell } from '@/modules/admin/ui/admin-page-shell';
import { AgentsTable } from '@/modules/admin/ui/agents-table';

export default async function AdminAgentsPage() {
  await requireGlobalAdmin();
  const heartbeats = await listAgentHeartbeats();
  return (
    <AdminPageShell>
      <AgentsTable heartbeats={heartbeats} />
    </AdminPageShell>
  );
}
